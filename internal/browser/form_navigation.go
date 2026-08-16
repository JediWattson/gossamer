package browser

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/loader"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

type DocumentRequest struct {
	URL    string
	Method string
	Header http.Header
	Body   []byte
}

// RequestDocumentLoader is the optional navigation seam for non-GET form
// submissions. Ordinary document navigation keeps using DocumentLoader.Load.
type RequestDocumentLoader interface {
	LoadRequest(context.Context, DocumentRequest) (*loader.Response, error)
}

type HistoryEntry struct {
	URL        *url.URL
	Navigation NavigationID
}

func (page *Page) SetFormNavigationLoader(client DocumentLoader) {
	if page == nil {
		return
	}
	page.mutex.Lock()
	page.formLoader = client
	page.mutex.Unlock()
}

func (page *Page) History() ([]HistoryEntry, int) {
	if page == nil {
		return nil, -1
	}
	page.mutex.RLock()
	entries := make([]HistoryEntry, len(page.history))
	for index, entry := range page.history {
		entries[index] = HistoryEntry{URL: cloneURL(entry.URL), Navigation: entry.Navigation}
	}
	current := page.historyIndex
	page.mutex.RUnlock()
	return entries, current
}

func (page *Page) pushHistoryLocked(location *url.URL, navigation NavigationID) {
	if location == nil {
		return
	}
	if page.historyIndex+1 < len(page.history) {
		page.history = page.history[:page.historyIndex+1]
	}
	page.history = append(page.history, HistoryEntry{URL: cloneURL(location), Navigation: navigation})
	page.historyIndex = len(page.history) - 1
}

// QueueRequestSubmit runs validation, invalid/submit event dispatch, and the
// navigation scheduling decision inside one Page task.
func (page *Page) QueueRequestSubmit(form, submitter NodeHandle) (browserruntime.TaskID, error) {
	if page == nil || page.browser == nil {
		return 0, fmt.Errorf("browser: nil page")
	}
	task, _, err := page.browser.scheduler.EnqueueExternalTask(page.Realm, func(context *browserruntime.TaskContext) error {
		return page.requestSubmitFromTask(context, form, submitter, true, true)
	})
	return task, err
}

func (page *Page) requestSubmitFromTask(
	task *browserruntime.TaskContext,
	form, submitter NodeHandle,
	validate bool,
	dispatchSubmit bool,
) error {
	page.mutex.RLock()
	if page.closed {
		page.mutex.RUnlock()
		return ErrPageClosed
	}
	if form.Document != page.documentGeneration || form.Node == dom.InvalidNodeID {
		page.mutex.RUnlock()
		return ErrStaleNodeHandle
	}
	if submitter.Node != dom.InvalidNodeID && submitter.Document != page.documentGeneration {
		page.mutex.RUnlock()
		return ErrStaleNodeHandle
	}
	submitterID := submitter.Node
	submission, err := page.document.PrepareFormSubmission(form.Node, submitterID)
	page.mutex.RUnlock()
	if err != nil {
		return err
	}
	host := &taskHost{page: page, task: task, generation: form.Document, autoRender: false}
	if validate && !submission.NoValidate {
		valid, invalid, validityErr := host.FormValidity(form)
		if validityErr != nil {
			return validityErr
		}
		if !valid {
			page.mutex.RLock()
			script := page.script
			page.mutex.RUnlock()
			if script != nil {
				for _, control := range invalid {
					_, validityErr = script.DispatchEvent(host, InputEvent{Type: InputInvalid, Target: control})
					if validityErr != nil {
						return validityErr
					}
				}
			}
			return nil
		}
	}
	if dispatchSubmit {
		page.mutex.RLock()
		script := page.script
		page.mutex.RUnlock()
		if script != nil {
			result, dispatchErr := script.DispatchEvent(host, InputEvent{Type: InputSubmit, Target: form})
			if dispatchErr != nil {
				return dispatchErr
			}
			if result.DefaultPrevented {
				return nil
			}
		}
	}
	return page.scheduleFormNavigation(submission)
}

func (page *Page) scheduleFormNavigation(submission dom.FormSubmission) error {
	page.mutex.RLock()
	base := cloneURL(page.location)
	client := page.formLoader
	page.mutex.RUnlock()
	if base == nil {
		return fmt.Errorf("browser: form submission has no base URL")
	}
	action := base
	if strings.TrimSpace(submission.Action) != "" {
		reference, err := url.Parse(submission.Action)
		if err != nil {
			return fmt.Errorf("browser: parse form action: %w", err)
		}
		action = base.ResolveReference(reference)
	}
	method := strings.ToUpper(strings.TrimSpace(submission.Method))
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet && method != http.MethodPost {
		method = http.MethodGet
	}
	if target := strings.TrimSpace(submission.Target); target != "" && !strings.EqualFold(target, "_self") {
		return fmt.Errorf("browser: unsupported form target %q", target)
	}
	encoded := encodeFormEntries(submission.Entries)
	request := DocumentRequest{URL: action.String(), Method: method, Header: make(http.Header)}
	if method == http.MethodGet {
		action.RawQuery = encoded
		request.URL = action.String()
	} else {
		enctype := strings.ToLower(strings.TrimSpace(submission.Enctype))
		if enctype == "" {
			enctype = "application/x-www-form-urlencoded"
		}
		switch enctype {
		case "application/x-www-form-urlencoded":
			request.Header.Set("Content-Type", enctype)
			request.Body = []byte(encoded)
		case "text/plain":
			request.Header.Set("Content-Type", enctype)
			lines := make([]string, len(submission.Entries))
			for index, entry := range submission.Entries {
				lines[index] = entry.Name + "=" + entry.Value
			}
			request.Body = []byte(strings.Join(lines, "\r\n"))
		default:
			return fmt.Errorf("browser: unsupported form enctype %q", enctype)
		}
	}
	if client == nil {
		client = loader.New(nil)
	}
	formLoader := DocumentLoader(client)
	if method != http.MethodGet {
		requestClient, ok := client.(RequestDocumentLoader)
		if !ok {
			return fmt.Errorf("browser: form loader does not support %s requests", method)
		}
		formLoader = formRequestLoader{client: requestClient, request: request}
	}
	_, err := page.Navigate(context.Background(), request.URL, formLoader)
	return err
}

func encodeFormEntries(entries []dom.FormEntry) string {
	parts := make([]string, len(entries))
	for index, entry := range entries {
		parts[index] = url.QueryEscape(entry.Name) + "=" + url.QueryEscape(entry.Value)
	}
	return strings.Join(parts, "&")
}

type formRequestLoader struct {
	client  RequestDocumentLoader
	request DocumentRequest
}

func (adapter formRequestLoader) Load(ctx context.Context, _ string) (*loader.Response, error) {
	request := adapter.request
	request.Header = request.Header.Clone()
	request.Body = append([]byte(nil), request.Body...)
	return adapter.client.LoadRequest(ctx, request)
}
