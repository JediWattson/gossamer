package sitecompat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/render"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

// InteractionMemoryProfiler records engine-neutral scenario checkpoints. The
// runner owns neither the profiler nor its output files.
type InteractionMemoryProfiler interface {
	Record(string, *browser.Page, bool) error
	RecordExecution(*browser.Page, *browserruntime.TaskContext, browserruntime.ExecutionSample) error
	WriteHeapProfile() error
}

// InteractionOptions supplies the production distribution and runtime-only
// values referenced by a declarative interaction scenario.
type InteractionOptions struct {
	Dist           string
	Definition     InteractionDefinition
	Variables      map[string]string
	MemoryProfiler InteractionMemoryProfiler
	PageObserver   func(*browser.Page)
}

// InteractionDefinition is a product-neutral description of a deterministic
// production-bundle interaction. Site-specific contracts belong in data, not
// in the browser harness.
type InteractionDefinition struct {
	Path          string                `json:"path,omitempty"`
	DOMLimit      int                   `json:"domLimit,omitempty"`
	Viewport      *InteractionViewport  `json:"viewport,omitempty"`
	AllowExternal bool                  `json:"allowExternal,omitempty"`
	Routes        []InteractionRoute    `json:"routes,omitempty"`
	Resources     []InteractionResource `json:"resources,omitempty"`
	Socket        InteractionSocket     `json:"socket,omitempty"`
	Steps         []InteractionStep     `json:"steps"`
}

type InteractionViewport struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type InteractionRoute struct {
	Method  string            `json:"method,omitempty"`
	Path    string            `json:"path"`
	Query   string            `json:"query,omitempty"`
	Status  int               `json:"status,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

type InteractionResource struct {
	URL     string            `json:"url"`
	Status  int               `json:"status,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

type InteractionSocket struct {
	OpenMessages []string          `json:"openMessages,omitempty"`
	Unlock       *InteractionMatch `json:"unlock,omitempty"`
}

// InteractionMatch compares decoded JSON values at dot-separated paths.
type InteractionMatch struct {
	Fields map[string]any `json:"fields"`
}

type InteractionCondition struct {
	ElementID       string            `json:"elementId,omitempty"`
	TextRootID      string            `json:"textRootId,omitempty"`
	ContainsText    string            `json:"containsText,omitempty"`
	SocketConnected bool              `json:"socketConnected,omitempty"`
	SocketUnlocked  bool              `json:"socketUnlocked,omitempty"`
	AcceptedMessage *InteractionMatch `json:"acceptedMessage,omitempty"`
}

type InteractionStep struct {
	Name       string                `json:"name"`
	Checkpoint string                `json:"checkpoint,omitempty"`
	Before     *InteractionCondition `json:"before,omitempty"`
	Script     string                `json:"script,omitempty"`
	After      *InteractionCondition `json:"after,omitempty"`
}

type InteractionStepReport struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
}

type InteractionHTTPReport struct {
	Requests []string `json:"requests,omitempty"`
}

type InteractionSocketReport struct {
	URL            string   `json:"url,omitempty"`
	CookieAttached bool     `json:"cookieAttached"`
	Events         []string `json:"events,omitempty"`
	Writes         int      `json:"writes"`
	Accepted       int      `json:"accepted"`
	Blocked        int      `json:"blocked"`
	Unlocked       bool     `json:"unlocked"`
}

type InteractionReport struct {
	Passed     bool                       `json:"passed"`
	URL        string                     `json:"url,omitempty"`
	Navigation browser.NavigationSnapshot `json:"navigation"`
	Steps      []InteractionStepReport    `json:"steps,omitempty"`
	HTTP       InteractionHTTPReport      `json:"http"`
	WebSocket  InteractionSocketReport    `json:"webSocket"`
	DOM        []string                   `json:"dom,omitempty"`
	Ownership  ownership.Stats            `json:"ownership"`
	Teardown   ownership.Stats            `json:"teardown"`
	Failure    string                     `json:"failure,omitempty"`
}

// RunInteraction executes one declarative scenario against a locally served
// production distribution. The runner owns and closes engine.
func RunInteraction(ctx context.Context, engine browser.Engine, options InteractionOptions) (report InteractionReport, resultErr error) {
	if engine == nil {
		return report, fmt.Errorf("sitecompat: nil interaction engine")
	}
	if ctx == nil {
		return report, errors.Join(fmt.Errorf("sitecompat: nil interaction context"), engine.Close())
	}
	root, err := filepath.Abs(strings.TrimSpace(options.Dist))
	if err != nil {
		return report, errors.Join(err, engine.Close())
	}
	info, err := os.Stat(root)
	if err != nil {
		return report, errors.Join(fmt.Errorf("sitecompat: inspect interaction dist: %w", err), engine.Close())
	}
	if !info.IsDir() {
		return report, errors.Join(fmt.Errorf("sitecompat: interaction dist %q is not a directory", root), engine.Close())
	}
	if err := validateInteractionDefinition(options.Definition); err != nil {
		return report, errors.Join(err, engine.Close())
	}

	fixture := &interactionServer{root: root, routes: append([]InteractionRoute(nil), options.Definition.Routes...)}
	server := httptest.NewServer(fixture)
	defer server.Close()
	client := &interactionLoader{
		base: loader.New(nil), origin: server.URL, allowExternal: options.Definition.AllowExternal,
		resources: append([]InteractionResource(nil), options.Definition.Resources...),
	}
	dialer := newInteractionDialer(options.Definition.Socket)
	report.URL = server.URL + interactionPath(options.Definition.Path)

	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		return report, errors.Join(err, engine.Close())
	}
	page, err := browserRuntime.NewPage(dom.NewDocument(), nil)
	if err == nil && options.Definition.Viewport != nil {
		err = page.SetViewport(render.Viewport{Width: options.Definition.Viewport.Width, Height: options.Definition.Viewport.Height})
	}
	if err == nil && options.PageObserver != nil {
		options.PageObserver(page)
	}
	if err == nil {
		page.SetWebSocketDialer(dialer)
		page.SetNavigationLoader(client)
		err = recordInteractionMemory(options.MemoryProfiler, "page-created", page)
	}
	if err == nil {
		var navigation browser.NavigationID
		navigation, err = page.Navigate(ctx, report.URL, client)
		if err == nil {
			err = page.WaitNavigation(ctx, navigation)
		}
	}
	if err == nil {
		err = recordInteractionMemory(options.MemoryProfiler, "navigation-complete", page)
	}
	if err == nil {
		snapshot := page.Navigation()
		if snapshot.ScriptsFailed != 0 {
			err = fmt.Errorf("sitecompat: initial interaction script failure: %s", snapshot.ScriptFailures[0].Message)
		}
	}
	if err == nil {
		for _, step := range options.Definition.Steps {
			stepErr := runInteractionStep(ctx, page, dialer, step, options.Variables)
			report.Steps = append(report.Steps, InteractionStepReport{Name: step.Name, Passed: stepErr == nil})
			if stepErr != nil {
				err = fmt.Errorf("sitecompat: interaction step %q: %w", step.Name, stepErr)
				break
			}
			if step.Checkpoint != "" {
				err = recordInteractionMemory(options.MemoryProfiler, step.Checkpoint, page)
				if err != nil {
					break
				}
			}
		}
	}
	if page != nil {
		err = errors.Join(err, recordInteractionMemory(options.MemoryProfiler, "scenario-complete", page))
		if options.MemoryProfiler != nil {
			err = errors.Join(err, options.MemoryProfiler.WriteHeapProfile())
		}
		report.Navigation = page.Navigation()
		domLimit := options.Definition.DOMLimit
		if domLimit == 0 {
			domLimit = 120
		}
		if domLimit > 0 {
			var domErr error
			report.DOM, domErr = page.InspectorDOMLines(domLimit)
			err = errors.Join(err, domErr)
		}
	}
	report.HTTP = fixture.report()
	report.WebSocket = dialer.report()
	report.Ownership = browserRuntime.Ledger().Stats()
	if page != nil {
		err = errors.Join(err, page.Close())
	}
	err = errors.Join(err, browserRuntime.Close())
	report.Teardown = browserRuntime.Ledger().Stats()
	if report.Teardown.LiveObjects != 0 || report.Teardown.PersistentObjects != 0 {
		err = errors.Join(err, fmt.Errorf("sitecompat: interaction teardown ownership = %#v", report.Teardown))
	}
	if err == nil && (report.Navigation.State != browser.NavigationComplete || report.Navigation.ScriptsFailed != 0) {
		err = fmt.Errorf("sitecompat: interaction navigation did not complete cleanly")
	}
	report.Passed = err == nil
	if err != nil {
		report.Failure = err.Error()
	}
	return report, err
}

func validateInteractionDefinition(definition InteractionDefinition) error {
	if len(definition.Steps) == 0 {
		return fmt.Errorf("sitecompat: interaction has no steps")
	}
	if definition.Viewport != nil && (definition.Viewport.Width <= 0 || definition.Viewport.Height <= 0) {
		return fmt.Errorf("sitecompat: interaction viewport must have positive width and height")
	}
	for index, route := range definition.Routes {
		if !strings.HasPrefix(strings.TrimSpace(route.Path), "/") {
			return fmt.Errorf("sitecompat: route %d path must be absolute", index)
		}
	}
	for index, resource := range definition.Resources {
		location, err := url.Parse(strings.TrimSpace(resource.URL))
		if err != nil || !location.IsAbs() {
			return fmt.Errorf("sitecompat: resource %d has invalid absolute URL", index)
		}
	}
	for index, step := range definition.Steps {
		if strings.TrimSpace(step.Name) == "" {
			return fmt.Errorf("sitecompat: interaction step %d has no name", index)
		}
		if step.Before == nil && strings.TrimSpace(step.Script) == "" && step.After == nil {
			return fmt.Errorf("sitecompat: interaction step %q has no operation", step.Name)
		}
	}
	return nil
}

func interactionPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	if !strings.HasPrefix(value, "/") {
		return "/" + value
	}
	return value
}

func runInteractionStep(ctx context.Context, page *browser.Page, dialer *interactionDialer, step InteractionStep, variables map[string]string) error {
	if step.Before != nil {
		if err := waitForInteraction(ctx, page, dialer, *step.Before); err != nil {
			return fmt.Errorf("before: %w", err)
		}
	}
	if strings.TrimSpace(step.Script) != "" {
		source, err := expandInteractionScript(step.Script, variables)
		if err != nil {
			return err
		}
		if _, err := page.QueueScript(browser.ScriptSource{URL: "interaction://" + url.PathEscape(step.Name) + ".js", Source: source}); err != nil {
			return err
		}
		if step.After == nil {
			if err := page.Realm.RunOne(ctx); err != nil {
				return err
			}
		}
	}
	if step.After != nil {
		if err := waitForInteraction(ctx, page, dialer, *step.After); err != nil {
			return fmt.Errorf("after: %w", err)
		}
	}
	return nil
}

func expandInteractionScript(source string, variables map[string]string) (string, error) {
	for name, value := range variables {
		encoded, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		source = strings.ReplaceAll(source, "{{"+name+"}}", string(encoded))
	}
	if start := strings.Index(source, "{{"); start >= 0 {
		if end := strings.Index(source[start+2:], "}}"); end >= 0 {
			return "", fmt.Errorf("sitecompat: unresolved interaction variable %q", source[start:start+end+4])
		}
	}
	return source, nil
}

func waitForInteraction(ctx context.Context, page *browser.Page, dialer *interactionDialer, condition InteractionCondition) error {
	for !interactionReady(page, dialer, condition) {
		if err := ctx.Err(); err != nil {
			return err
		}
		if page.Realm.Tasks.Len() != 0 {
			if err := page.Realm.RunOne(ctx); err != nil {
				return err
			}
			continue
		}
		timer := time.NewTimer(2 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}

func interactionReady(page *browser.Page, dialer *interactionDialer, condition InteractionCondition) bool {
	document := page.Document()
	if condition.ElementID != "" {
		if _, found := document.ElementByID(condition.ElementID); !found {
			return false
		}
	}
	if condition.TextRootID != "" || condition.ContainsText != "" {
		rootID := condition.TextRootID
		if rootID == "" {
			return false
		}
		root, found := document.ElementByID(rootID)
		if !found {
			return false
		}
		text, err := document.TextContent(root)
		if err != nil || !strings.Contains(text, condition.ContainsText) {
			return false
		}
	}
	snapshot := dialer.snapshot()
	if condition.SocketConnected && snapshot.url == "" {
		return false
	}
	if condition.SocketUnlocked && !snapshot.unlocked {
		return false
	}
	if condition.AcceptedMessage != nil && !matchesAnyInteractionMessage(snapshot.accepted, *condition.AcceptedMessage) {
		return false
	}
	return true
}

func recordInteractionMemory(profiler InteractionMemoryProfiler, label string, page *browser.Page) error {
	if profiler == nil || page == nil {
		return nil
	}
	return profiler.Record(label, page, true)
}

type interactionServer struct {
	root     string
	routes   []InteractionRoute
	mutex    sync.Mutex
	requests []string
}

func (server *interactionServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	server.mutex.Lock()
	server.requests = append(server.requests, request.Method+" "+request.URL.Path)
	server.mutex.Unlock()
	for _, route := range server.routes {
		method := strings.ToUpper(strings.TrimSpace(route.Method))
		if method == "" {
			method = http.MethodGet
		}
		if request.Method != method || request.URL.Path != route.Path || route.Query != "" && request.URL.RawQuery != route.Query {
			continue
		}
		for name, value := range route.Headers {
			writer.Header().Set(name, value)
		}
		status := route.Status
		if status == 0 {
			status = http.StatusOK
		}
		writer.WriteHeader(status)
		_, _ = io.WriteString(writer, route.Body)
		return
	}
	if request.Method == http.MethodGet && filepath.Ext(request.URL.Path) == "" {
		http.ServeFile(writer, request, filepath.Join(server.root, "index.html"))
		return
	}
	http.FileServer(http.Dir(server.root)).ServeHTTP(writer, request)
}

func (server *interactionServer) report() InteractionHTTPReport {
	server.mutex.Lock()
	defer server.mutex.Unlock()
	return InteractionHTTPReport{Requests: append([]string(nil), server.requests...)}
}

type interactionLoader struct {
	base          *loader.Loader
	origin        string
	allowExternal bool
	resources     []InteractionResource
}

func (client *interactionLoader) Load(ctx context.Context, rawURL string) (*loader.Response, error) {
	return client.base.Load(ctx, rawURL)
}

func (client *interactionLoader) LoadResource(ctx context.Context, rawURL string, destination loader.Destination) (*loader.Response, error) {
	for _, resource := range client.resources {
		if rawURL != resource.URL {
			continue
		}
		location, _ := url.Parse(rawURL)
		header := make(http.Header)
		for name, value := range resource.Headers {
			header.Set(name, value)
		}
		status := resource.Status
		if status == 0 {
			status = http.StatusOK
		}
		return &loader.Response{URL: location, StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(resource.Body))}, nil
	}
	if !client.allowExternal && !sameInteractionOrigin(rawURL, client.origin) {
		return nil, fmt.Errorf("sitecompat: unconfigured external interaction resource %q", rawURL)
	}
	return client.base.LoadResource(ctx, rawURL, destination)
}

func (client *interactionLoader) Do(ctx context.Context, request loader.Request) (*loader.Response, error) {
	if !client.allowExternal && !sameInteractionOrigin(request.URL, client.origin) {
		return nil, fmt.Errorf("sitecompat: unconfigured external interaction request %q", request.URL)
	}
	return client.base.Do(ctx, request)
}

func (client *interactionLoader) Cookies(location *url.URL) []*http.Cookie {
	return client.base.Cookies(location)
}

func (client *interactionLoader) SetCookies(location *url.URL, cookies []*http.Cookie) {
	client.base.SetCookies(location, cookies)
}

func sameInteractionOrigin(rawURL, origin string) bool {
	location, locationErr := url.Parse(rawURL)
	base, baseErr := url.Parse(origin)
	return locationErr == nil && baseErr == nil && location.Scheme == base.Scheme && location.Host == base.Host
}

type interactionSocketSnapshot struct {
	url      string
	cookie   string
	writes   []string
	accepted []string
	blocked  int
	unlocked bool
}

type interactionDialer struct {
	mutex    sync.Mutex
	fixture  InteractionSocket
	url      string
	cookie   string
	writes   []string
	accepted []string
	blocked  int
	unlocked bool
	reads    chan interactionRead
}

type interactionRead struct {
	message browser.WebSocketMessageType
	data    []byte
	err     error
}

func newInteractionDialer(fixture InteractionSocket) *interactionDialer {
	capacity := len(fixture.OpenMessages) + 1
	return &interactionDialer{fixture: fixture, unlocked: fixture.Unlock == nil, reads: make(chan interactionRead, capacity)}
}

func (dialer *interactionDialer) Dial(_ context.Context, rawURL string, _ []string, header http.Header) (browser.WebSocketConnection, error) {
	location, err := url.Parse(rawURL)
	if err != nil || location.Scheme != "ws" && location.Scheme != "wss" {
		return nil, fmt.Errorf("sitecompat: invalid interaction WebSocket URL %q", rawURL)
	}
	dialer.mutex.Lock()
	dialer.url = rawURL
	dialer.cookie = header.Get("Cookie")
	messages := append([]string(nil), dialer.fixture.OpenMessages...)
	dialer.mutex.Unlock()
	for _, raw := range messages {
		dialer.reads <- interactionRead{message: browser.WebSocketTextMessage, data: []byte(raw)}
	}
	return &interactionConnection{dialer: dialer}, nil
}

func (dialer *interactionDialer) snapshot() interactionSocketSnapshot {
	dialer.mutex.Lock()
	defer dialer.mutex.Unlock()
	return interactionSocketSnapshot{
		url: dialer.url, cookie: dialer.cookie, writes: append([]string(nil), dialer.writes...),
		accepted: append([]string(nil), dialer.accepted...), blocked: dialer.blocked, unlocked: dialer.unlocked,
	}
}

func (dialer *interactionDialer) report() InteractionSocketReport {
	snapshot := dialer.snapshot()
	events := make([]string, 0, len(snapshot.writes))
	for _, raw := range snapshot.writes {
		var envelope map[string]any
		if json.Unmarshal([]byte(raw), &envelope) == nil {
			if event, _ := envelope["event"].(string); event != "" {
				events = append(events, event)
			}
		}
	}
	return InteractionSocketReport{
		URL: snapshot.url, CookieAttached: strings.TrimSpace(snapshot.cookie) != "", Events: events,
		Writes: len(snapshot.writes), Accepted: len(snapshot.accepted), Blocked: snapshot.blocked, Unlocked: snapshot.unlocked,
	}
}

type interactionConnection struct{ dialer *interactionDialer }

func (connection *interactionConnection) Read(ctx context.Context) (browser.WebSocketMessageType, []byte, error) {
	select {
	case item := <-connection.dialer.reads:
		return item.message, item.data, item.err
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

func (connection *interactionConnection) Write(_ context.Context, message browser.WebSocketMessageType, data []byte) error {
	if message != browser.WebSocketTextMessage {
		return fmt.Errorf("sitecompat: interaction expected text WebSocket message")
	}
	raw := string(data)
	dialer := connection.dialer
	dialer.mutex.Lock()
	defer dialer.mutex.Unlock()
	dialer.writes = append(dialer.writes, raw)
	if !dialer.unlocked {
		if dialer.fixture.Unlock != nil && matchesInteractionMessage(raw, *dialer.fixture.Unlock) {
			dialer.unlocked = true
		} else {
			dialer.blocked++
		}
		return nil
	}
	dialer.accepted = append(dialer.accepted, raw)
	return nil
}

func (*interactionConnection) Close(_ uint16, _ string) error { return nil }
func (*interactionConnection) Protocol() string               { return "" }

func matchesAnyInteractionMessage(messages []string, match InteractionMatch) bool {
	for _, raw := range messages {
		if matchesInteractionMessage(raw, match) {
			return true
		}
	}
	return false
}

func matchesInteractionMessage(raw string, match InteractionMatch) bool {
	if len(match.Fields) == 0 {
		return false
	}
	var value any
	if json.Unmarshal([]byte(raw), &value) != nil {
		return false
	}
	for path, expected := range match.Fields {
		actual, found := interactionJSONPath(value, path)
		if !found || !reflect.DeepEqual(actual, expected) {
			return false
		}
	}
	return true
}

func interactionJSONPath(value any, path string) (any, bool) {
	current := value
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = object[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
