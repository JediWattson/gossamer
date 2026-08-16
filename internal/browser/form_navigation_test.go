package browser_test

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/loader"
)

func TestFormRequestSubmitValidatesNavigatesAndReplacesDocument(t *testing.T) {
	t.Parallel()

	engine, err := browser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	root, err := parseDocument(`<html><body><form action="/search"><input name="q" required><button name="commit" value="yes">Go</button></form></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	initial, _ := url.Parse("https://example.test/start")
	page, err := engine.NewPage(root, initial)
	if err != nil {
		t.Fatal(err)
	}
	form := findElement(root, "form")
	input := findElement(root, "input")
	button := findElement(root, "button")
	formID, _ := page.Document().ID(form)
	inputID, _ := page.Document().ID(input)
	buttonID, _ := page.Document().ID(button)
	formHandle := browser.NodeHandle{Document: page.DocumentGeneration(), Node: formID}
	submitterHandle := browser.NodeHandle{Document: page.DocumentGeneration(), Node: buttonID}
	client := &recordingFormLoader{document: `<html><body><p>search results</p></body></html>`}
	page.SetFormNavigationLoader(client)

	if _, err := page.QueueRequestSubmit(formHandle, submitterHandle); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.requestCount() != 0 || page.Navigation().ID != 0 {
		t.Fatal("invalid form submission started navigation")
	}

	if err := page.Document().SetFormValue(inputID, "gossamer"); err != nil {
		t.Fatal(err)
	}
	oldGeneration := page.DocumentGeneration()
	if _, err := page.QueueRequestSubmit(formHandle, submitterHandle); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	navigation := page.Navigation().ID
	if navigation == 0 {
		t.Fatal("valid form submission did not schedule navigation")
	}
	if err := page.WaitNavigation(context.Background(), navigation); err != nil {
		t.Fatal(err)
	}
	request := client.latestRequest()
	if request != "https://example.test/search?q=gossamer&commit=yes" {
		t.Fatalf("form request = %q", request)
	}
	if page.DocumentGeneration() == oldGeneration {
		t.Fatal("successful form navigation retained the old document generation")
	}
	if _, ok := page.Resolve(formHandle); ok {
		t.Fatal("old form handle resolved after document replacement")
	}
	if !frameContainsText(page.Frame(), "search results") {
		t.Fatal("successful form navigation did not paint the response")
	}
	history, current := page.History()
	if current != len(history)-1 || len(history) != 2 || history[current].URL.String() != request {
		t.Fatalf("history = %#v, current %d", history, current)
	}
}

func TestPostFormSubmissionUsesRequestLoader(t *testing.T) {
	t.Parallel()

	engine, err := browser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	root, err := parseDocument(`<html><body><form action="/save" method="post"><input name="title" value="memory architecture"></form></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	initial, _ := url.Parse("https://example.test/editor")
	page, err := engine.NewPage(root, initial)
	if err != nil {
		t.Fatal(err)
	}
	formID, _ := page.Document().ID(findElement(root, "form"))
	formHandle := browser.NodeHandle{Document: page.DocumentGeneration(), Node: formID}
	client := &recordingFormLoader{document: `<html><body><p>saved</p></body></html>`}
	page.SetFormNavigationLoader(client)
	if _, err := page.QueueRequestSubmit(formHandle, browser.NodeHandle{}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	navigation := page.Navigation().ID
	if err := page.WaitNavigation(context.Background(), navigation); err != nil {
		t.Fatal(err)
	}
	request := client.latestDocumentRequest()
	if request.Method != http.MethodPost || request.URL != "https://example.test/save" || string(request.Body) != "title=memory+architecture" {
		t.Fatalf("POST form request = %#v", request)
	}
	if request.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
		t.Fatalf("Content-Type = %q", request.Header.Get("Content-Type"))
	}
}

type recordingFormLoader struct {
	mutex    sync.Mutex
	document string
	requests []string
	posts    []browser.DocumentRequest
}

func (client *recordingFormLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	client.mutex.Lock()
	client.requests = append(client.requests, rawURL)
	client.mutex.Unlock()
	return formDocumentResponse(rawURL, client.document)
}

func (client *recordingFormLoader) LoadRequest(_ context.Context, request browser.DocumentRequest) (*loader.Response, error) {
	request.Header = request.Header.Clone()
	request.Body = append([]byte(nil), request.Body...)
	client.mutex.Lock()
	client.posts = append(client.posts, request)
	client.mutex.Unlock()
	return formDocumentResponse(request.URL, client.document)
}

func (client *recordingFormLoader) requestCount() int {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return len(client.requests) + len(client.posts)
}

func (client *recordingFormLoader) latestRequest() string {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if len(client.requests) == 0 {
		return ""
	}
	return client.requests[len(client.requests)-1]
}

func (client *recordingFormLoader) latestDocumentRequest() browser.DocumentRequest {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	if len(client.posts) == 0 {
		return browser.DocumentRequest{}
	}
	return client.posts[len(client.posts)-1]
}

func formDocumentResponse(rawURL, document string) (*loader.Response, error) {
	location, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	return &loader.Response{
		URL:        location,
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:       io.NopCloser(strings.NewReader(document)),
	}, nil
}

func findElement(root *dom.Node, name string) *dom.Node {
	if root == nil {
		return nil
	}
	if root.Type == dom.ElementNode && root.Data == name {
		return root
	}
	for _, child := range root.Children {
		if found := findElement(child, name); found != nil {
			return found
		}
	}
	return nil
}
