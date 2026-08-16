package window

import (
	"context"
	"image"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/browser/fake"
)

func TestGraphiteInspectorPanelsUseCopiedBrowserSnapshots(t *testing.T) {
	t.Parallel()

	page, closePage := newShellTestPage(t, shellTestLoader{document: `<html><body><div id="target" style="display:block;width:120px;height:40px">Inspect me</div></body></html>`})
	defer closePage()
	lines, err := page.InspectorDOMLines(20)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(lines, func(line string) bool { return strings.Contains(line, `id="target"`) }) {
		t.Fatalf("DOM inspector lines = %q, want target", lines)
	}
	targetID, found := page.Document().ElementByID("target")
	if !found {
		t.Fatal("target has no stable node ID")
	}
	target := browser.NodeHandle{Document: page.DocumentGeneration(), Node: targetID}
	node, err := page.InspectorNode(target)
	if err != nil || node.Name != "div" || len(node.Attributes) == 0 {
		t.Fatalf("inspector node = %#v err=%v", node, err)
	}

	shell, err := newGraphiteShell(page, ShellConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer shell.close()
	shell.initialWindowSize(page.Frame().Viewport)
	shell.inspectorOpen = true
	shell.activeInputState().hoverTarget = target
	pageCanvas := image.NewRGBA(image.Rect(0, 0, page.Frame().Viewport.Width, page.Frame().Viewport.Height))
	for panel := inspectorDOM; panel <= inspectorMemory; panel++ {
		shell.inspectorPanel = panel
		if _, err := shell.compose(pageCanvas, page); err != nil {
			t.Fatalf("compose inspector panel %d: %v", panel, err)
		}
	}
}

func TestGraphiteNavigationProgressUsesRealPhaseCounts(t *testing.T) {
	t.Parallel()

	if got := shellNavigationProgress(browser.NavigationSnapshot{State: browser.NavigationLoadingDocument}); got != 0.12 {
		t.Fatalf("document progress = %v", got)
	}
	resources := shellNavigationProgress(browser.NavigationSnapshot{
		State: browser.NavigationLoadingResources, ResourcesTotal: 4, ResourcesPending: 2,
	})
	if resources <= 0.20 || resources >= 0.55 {
		t.Fatalf("mid-resource progress = %v", resources)
	}
	scripts := shellNavigationProgress(browser.NavigationSnapshot{
		State: browser.NavigationLoadingScripts, ScriptsTotal: 2, ScriptsPending: 0,
	})
	if scripts != 0.82 {
		t.Fatalf("complete-script progress = %v", scripts)
	}
	if got := shellNavigationProgress(browser.NavigationSnapshot{State: browser.NavigationComplete}); got != 1 {
		t.Fatalf("complete progress = %v", got)
	}
}

func TestGraphiteTabOverflowAndReorderPreserveActiveIdentity(t *testing.T) {
	t.Parallel()

	page, closePage := newShellTestPage(t, shellTestLoader{document: `<html><body>tabs</body></html>`})
	defer closePage()
	shell, err := newGraphiteShell(page, ShellConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer shell.close()
	first := shell.tabs[0]
	for index := 1; index < maximumGraphiteTabs; index++ {
		duplicate := first
		duplicate.state.address = "https://tab-" + fmtInt(index) + ".gossamer.test/"
		shell.tabs = append(shell.tabs, duplicate)
	}
	shell.activeTab = 5
	shell.width = 520
	shell.height = 400
	layout := shell.layout()
	if layout.tabOverflowBack.Empty() || layout.tabOverflowNext.Empty() || layout.tabs[shell.activeTab].body.Empty() {
		t.Fatalf("overflow layout = back %v next %v active %v", layout.tabOverflowBack, layout.tabOverflowNext, layout.tabs[shell.activeTab].body)
	}
	activeAddress := shell.tabs[shell.activeTab].state.address
	shell.moveTab(shell.activeTab, 1)
	if shell.activeTab != 1 || shell.tabs[1].state.address != activeAddress {
		t.Fatalf("reordered active tab = index %d address %q", shell.activeTab, shell.tabs[1].state.address)
	}
}

func TestTextControlPresentationAndNestedWheelScrolling(t *testing.T) {
	t.Parallel()

	page, closePage := newShellTestPage(t, shellTestLoader{document: `<html><body style="margin:0"><input id="field" type="password" value="secret"><div id="scroll" style="display:block;width:160px;height:80px;overflow:auto"><div style="display:block;height:400px">inside</div></div></body></html>`})
	defer closePage()
	fieldID, found := page.Document().ElementByID("field")
	if !found {
		t.Fatal("field has no stable ID")
	}
	if err := page.Document().SetFormSelection(fieldID, 1, 4, "forward"); err != nil {
		t.Fatal(err)
	}
	presentation, err := page.TextControlPresentation(browser.NodeHandle{Document: page.DocumentGeneration(), Node: fieldID})
	if err != nil || presentation.Value != "secret" || presentation.SelectionStart != 1 || presentation.SelectionEnd != 4 || !presentation.Password {
		t.Fatalf("text control presentation = %#v err=%v", presentation, err)
	}
	scrollID, found := page.Document().ElementByID("scroll")
	if !found {
		t.Fatal("scroll container has no stable ID")
	}
	scroll := browser.NodeHandle{Document: page.DocumentGeneration(), Node: scrollID}
	geometry, err := page.ElementGeometry(scroll)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScrollAt(geometry.Rect.X+4, geometry.Rect.Y+4, 0, 50); err != nil {
		t.Fatal(err)
	}
	for page.Realm.Tasks.Len() != 0 {
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	after, err := page.ElementGeometry(scroll)
	if err != nil {
		t.Fatal(err)
	}
	if after.ScrollTop <= 0 {
		t.Fatalf("nested scrollTop = %v, want positive", after.ScrollTop)
	}
}

func TestGraphiteContextMenuCopiesAndDownloadsHitTestedLink(t *testing.T) {
	t.Parallel()

	page, closePage := newShellTestPage(t, shellTestLoader{document: `<html><body style="margin:0"><a id="link" href="/files/report.txt" style="display:block;width:180px;height:40px">report</a></body></html>`})
	defer closePage()
	var downloaded DownloadRequest
	shell, err := newGraphiteShell(page, ShellConfig{Download: func(_ context.Context, request DownloadRequest) error {
		downloaded = request
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer shell.close()
	shell.initialWindowSize(page.Frame().Viewport)
	linkID, _ := page.Document().ElementByID("link")
	geometry, err := page.ElementGeometry(browser.NodeHandle{Document: page.DocumentGeneration(), Node: linkID})
	if err != nil {
		t.Fatal(err)
	}
	event := Event{Kind: EventPointerDown, Button: 1, X: geometry.Rect.X + 4, Y: float64(graphiteChromeHeight) + geometry.Rect.Y + 4}
	backend := NewMemoryBackend()
	backend.QueueContextAction(ContextActionCopyLink)
	if handled, err := shell.handleContextMenu(context.Background(), page, backend, event); err != nil || !handled {
		t.Fatalf("copy context menu handled=%t err=%v", handled, err)
	}
	if got := backend.ClipboardText(); got != "https://start.gossamer.test/files/report.txt" {
		t.Fatalf("copied link = %q", got)
	}
	backend.QueueContextAction(ContextActionDownload)
	if handled, err := shell.handleContextMenu(context.Background(), page, backend, event); err != nil || !handled {
		t.Fatalf("download context menu handled=%t err=%v", handled, err)
	}
	if downloaded.URL == nil || downloaded.URL.String() != "https://start.gossamer.test/files/report.txt" || downloaded.SuggestedName != "report.txt" {
		t.Fatalf("download request = %#v", downloaded)
	}
}

func TestGraphiteKeyboardFocusAndAccessibilitySnapshot(t *testing.T) {
	t.Parallel()

	page, closePage := newShellTestPage(t, shellTestLoader{document: `<html><head><title>Accessible page</title></head><body>content</body></html>`})
	defer closePage()
	shell, err := newGraphiteShell(page, ShellConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer shell.close()
	shell.initialWindowSize(page.Frame().Viewport)

	handled, _, _, err := shell.handleEvent(context.Background(), page, Event{
		Kind: EventKeyDown, Key: "F6", Code: "F6", Modifiers: Modifiers{Ctrl: true},
	}, shell.activeInputState())
	if err != nil || !handled || shell.chromeFocus != shellFocusTab {
		t.Fatalf("Control-F6 handled=%t focus=%d err=%v", handled, shell.chromeFocus, err)
	}
	if handled, _, _, err = shell.handleEvent(context.Background(), page, Event{
		Kind: EventKeyDown, Key: "Tab", Code: "Tab",
	}, shell.activeInputState()); err != nil || !handled || shell.chromeFocus != shellFocusBack {
		t.Fatalf("chrome Tab handled=%t focus=%d err=%v", handled, shell.chromeFocus, err)
	}
	shell.chromeFocus = shellFocusInspectorMemory
	if handled, _, _, err = shell.handleEvent(context.Background(), page, Event{
		Kind: EventKeyDown, Key: "Enter", Code: "Enter",
	}, shell.activeInputState()); err != nil || !handled || !shell.inspectorOpen || shell.inspectorPanel != inspectorMemory {
		t.Fatalf("memory inspector activation handled=%t open=%t panel=%d err=%v", handled, shell.inspectorOpen, shell.inspectorPanel, err)
	}

	snapshot := shell.accessibilitySnapshot()
	if snapshot.Revision != shell.revision || len(snapshot.Nodes) < 10 {
		t.Fatalf("accessibility snapshot revision=%d nodes=%d", snapshot.Revision, len(snapshot.Nodes))
	}
	if !slices.ContainsFunc(snapshot.Nodes, func(node AccessibilityNode) bool {
		return node.ID == "address" && node.Role == AccessibilityTextField && node.Value == "https://start.gossamer.test/"
	}) {
		t.Fatalf("accessibility nodes have no live address field: %#v", snapshot.Nodes)
	}
	if !slices.ContainsFunc(snapshot.Nodes, func(node AccessibilityNode) bool {
		return node.ID == "inspector-4" && node.Selected && node.Focused
	}) {
		t.Fatalf("memory inspector accessibility state missing: %#v", snapshot.Nodes)
	}
}

func TestGraphiteSessionRestoreRebuildsTabsAndRoundTripsState(t *testing.T) {
	t.Parallel()

	engine := fake.New()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	client := shellTestLoader{document: `<html><body>restored</body></html>`}
	initialPage, err := browserRuntime.LoadPage(context.Background(), "https://one.gossamer.test/", client)
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemorySessionStore(SessionSnapshot{
		Version: sessionSnapshotVersion, Tabs: []string{"https://one.gossamer.test/", "https://two.gossamer.test/path"},
		ActiveTab: 1, InspectorOpen: true, InspectorPanel: uint8(inspectorNetwork),
	})
	shell, err := newGraphiteShell(initialPage, ShellConfig{
		Loader: client, OpenTab: browserRuntime.NewBlankPage, Session: store,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer shell.close()
	shell.initialWindowSize(initialPage.Frame().Viewport)
	if err := shell.loadSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := shell.restoreSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(shell.tabs) != 2 || shell.activeTab != 1 || !shell.inspectorOpen || shell.inspectorPanel != inspectorNetwork {
		t.Fatalf("restored shell = tabs %d active %d inspector %t/%d", len(shell.tabs), shell.activeTab, shell.inspectorOpen, shell.inspectorPanel)
	}
	if err := shell.saveSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	saved, ok := store.Snapshot()
	if !ok || saved.Version != sessionSnapshotVersion || saved.ActiveTab != 1 || len(saved.Tabs) != 2 || saved.Tabs[1] != "https://two.gossamer.test/path" {
		t.Fatalf("saved session = %#v present=%t", saved, ok)
	}
}

func TestFileSessionStoreRoundTrip(t *testing.T) {
	t.Parallel()

	store := FileSessionStore{Path: filepath.Join(t.TempDir(), "state", "session.json")}
	want := SessionSnapshot{Version: sessionSnapshotVersion, Tabs: []string{"https://gossamer.test/"}, InspectorOpen: true}
	if err := store.SaveSession(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	got, err := store.LoadSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != want.Version || !slices.Equal(got.Tabs, want.Tabs) || !got.InspectorOpen {
		t.Fatalf("file session = %#v, want %#v", got, want)
	}
}

func TestRunBrowserPublishesAccessibilitySnapshots(t *testing.T) {
	t.Parallel()

	page, closePage := newShellTestPage(t, shellTestLoader{document: `<html><body>accessible</body></html>`})
	defer closePage()
	backend := NewMemoryBackend(Event{Kind: EventClose})
	if err := RunBrowser(context.Background(), page, backend, ShellConfig{}); err != nil {
		t.Fatal(err)
	}
	snapshots := backend.AccessibilitySnapshots()
	if len(snapshots) == 0 || len(snapshots[0].Nodes) == 0 {
		t.Fatalf("published accessibility snapshots = %#v", snapshots)
	}
}
