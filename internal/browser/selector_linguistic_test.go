package browser

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/loader"
)

func TestLinguisticSelectorsRestyleAfterLanguageTextAndValueMutations(t *testing.T) {
	t.Parallel()

	engine, page, targetID := computedStyleTestPage(t, `
		<html><head><style>
			#target { display:block; color:black; background-color:white }
			#target:lang(fr) { color:red }
			#target:lang(de-DE) { color:green }
			#target:dir(rtl) { background-color:blue }
			#scope { display:block; width:10px }
			#scope:has(> input:dir(rtl)) { width:30px }
		</style></head><body>
			<section id="scope" lang="fr">
				<p id="target" dir="auto">שלום</p>
				<input id="control" dir="auto" value="مرحبا">
			</section>
		</body></html>
	`)
	defer engine.Close()

	generation := page.DocumentGeneration()
	host := &taskHost{page: page, generation: generation}
	target := NodeHandle{Document: generation, Node: targetID}
	scopeID, ok := page.document.ElementByID("scope")
	if !ok {
		t.Fatal("scope has no stable ID")
	}
	controlID, ok := page.document.ElementByID("control")
	if !ok {
		t.Fatal("control has no stable ID")
	}
	scope := NodeHandle{Document: generation, Node: scopeID}
	control := NodeHandle{Document: generation, Node: controlID}
	root := NodeHandle{Document: generation, Node: page.document.RootID()}

	assertSelectorStateProperty(t, page, target, "color", "rgb(255, 0, 0)")
	assertSelectorStateProperty(t, page, target, "background-color", "rgb(0, 0, 255)")
	assertSelectorStateProperty(t, page, scope, "width", "30px")
	assertFormSelectorMatch(t, host, target, `:lang(fr):dir(rtl)`, true)
	assertFormSelectorMatch(t, host, control, `:dir(rtl)`, true)
	matches, err := host.QuerySelector(root, `#scope:lang(fr):has(> input:dir(rtl))`, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0] != scope {
		t.Fatalf("linguistic query = %#v, want scope", matches)
	}

	firstSnapshot := page.computedStyle.snapshot
	if err := host.SetAttribute(scope, "lang", "de-Latn-DE"); err != nil {
		t.Fatal(err)
	}
	if err := host.SetTextContent(target, "hello"); err != nil {
		t.Fatal(err)
	}
	if err := host.SetFormValue(control, "hello"); err != nil {
		t.Fatal(err)
	}
	assertSelectorStateProperty(t, page, target, "color", "rgb(0, 128, 0)")
	assertSelectorStateProperty(t, page, target, "background-color", "rgb(255, 255, 255)")
	assertSelectorStateProperty(t, page, scope, "width", "10px")
	assertFormSelectorMatch(t, host, target, `:lang(de-DE):dir(ltr)`, true)
	assertFormSelectorMatch(t, host, control, `:dir(ltr)`, true)
	if page.computedStyle.snapshot == firstSnapshot {
		t.Fatal("linguistic mutation did not replace the versioned style snapshot")
	}

	if err := host.SetAttribute(scope, "lang", ""); err != nil {
		t.Fatal(err)
	}
	assertFormSelectorMatch(t, host, target, `:lang("")`, true)
	assertFormSelectorMatch(t, host, target, `:lang("*")`, false)
	if page.Frame() != nil {
		t.Fatal("linguistic style flush unexpectedly published a frame")
	}
	if !page.Dirty() {
		t.Fatal("linguistic mutations did not leave the page dirty for task-boundary render")
	}
}

func TestNavigationContentLanguageFeedsLinguisticSelectors(t *testing.T) {
	t.Parallel()

	engine, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()

	pageURL, _ := url.Parse("https://language.test/single")
	source := `<style>
		#target { display:block; color:black }
		#target:lang(fr) { color:red }
	</style><p id="target">bonjour</p>`
	page, err := engine.LoadPage(context.Background(), pageURL.String(), linguisticDocumentLoader{
		url:      pageURL,
		source:   source,
		language: "fr-CA",
	})
	if err != nil {
		t.Fatal(err)
	}

	targetID, ok := page.document.ElementByID("target")
	if !ok {
		t.Fatal("target has no stable ID")
	}
	handle := NodeHandle{Document: page.DocumentGeneration(), Node: targetID}
	assertSelectorStateProperty(t, page, handle, "color", "rgb(255, 0, 0)")

	multipleURL, _ := url.Parse("https://language.test/multiple")
	navigation, err := page.Navigate(context.Background(), multipleURL.String(), linguisticDocumentLoader{
		url:      multipleURL,
		source:   source,
		language: "fr, de",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(context.Background(), navigation); err != nil {
		t.Fatal(err)
	}
	targetID, ok = page.document.ElementByID("target")
	if !ok {
		t.Fatal("replacement target has no stable ID")
	}
	handle = NodeHandle{Document: page.DocumentGeneration(), Node: targetID}
	assertSelectorStateProperty(t, page, handle, "color", "rgb(0, 0, 0)")
}

type linguisticDocumentLoader struct {
	url      *url.URL
	source   string
	language string
}

func (stub linguisticDocumentLoader) Load(context.Context, string) (*loader.Response, error) {
	header := make(http.Header)
	if stub.language != "" {
		header.Set("Content-Language", stub.language)
	}
	return &loader.Response{
		URL:        stub.url,
		StatusCode: http.StatusOK,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(stub.source)),
	}, nil
}
