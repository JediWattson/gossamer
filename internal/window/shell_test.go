package window

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"io"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/browser/fake"
	"github.com/JediWattson/gossamer/internal/loader"
)

func TestGraphiteShellComposesChromeContentRailAndInspector(t *testing.T) {
	t.Parallel()

	page, closePage := newShellTestPage(t, shellTestLoader{document: `<html><body>Graphite</body></html>`})
	defer closePage()
	shell, err := newGraphiteShell(page, ShellConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer shell.close()
	width, height := shell.initialWindowSize(page.Frame().Viewport)
	if width != 848 || height != 684 {
		t.Fatalf("initial shell size = %dx%d, want 848x684", width, height)
	}

	pageColor := color.NRGBA{R: 0xf3, G: 0x45, B: 0x67, A: 0xff}
	pageCanvas := image.NewRGBA(image.Rect(0, 0, 800, 600))
	draw.Draw(pageCanvas, pageCanvas.Bounds(), image.NewUniform(pageColor), image.Point{}, draw.Src)
	composed, err := shell.compose(pageCanvas, page)
	if err != nil {
		t.Fatal(err)
	}
	if composed.Bounds() != image.Rect(0, 0, 848, 684) {
		t.Fatalf("composed bounds = %v", composed.Bounds())
	}
	if got := color.NRGBAModel.Convert(composed.At(5, graphiteChromeHeight+5)).(color.NRGBA); got != pageColor {
		t.Fatalf("content pixel = %#v, want %#v", got, pageColor)
	}
	if got := color.NRGBAModel.Convert(composed.At(5, 5)).(color.NRGBA); got == pageColor {
		t.Fatal("tab row exposed the page canvas")
	}
	if got := color.NRGBAModel.Convert(composed.At(820, 300)).(color.NRGBA); got == pageColor {
		t.Fatal("collapsed rail exposed the page canvas")
	}
	layout := shell.layout()
	if got := color.NRGBAModel.Convert(composed.At(layout.tab.Min.X, layout.tab.Min.Y)).(color.NRGBA); got != graphitePalette.top {
		t.Fatalf("tab top corner = %#v, want rounded-through background %#v", got, graphitePalette.top)
	}
	if got := color.NRGBAModel.Convert(composed.At(layout.tab.Min.X, layout.tab.Max.Y-1)).(color.NRGBA); got != graphitePalette.tealDim {
		t.Fatalf("tab bottom corner = %#v, want square active edge %#v", got, graphitePalette.tealDim)
	}

	shell.inspectorOpen = true
	withInspector, err := shell.compose(pageCanvas, page)
	if err != nil {
		t.Fatal(err)
	}
	panel := shell.layout().inspector
	if panel.Empty() {
		t.Fatal("open inspector has no layout rectangle")
	}
	if got := color.NRGBAModel.Convert(withInspector.At(panel.Min.X+4, panel.Max.Y-20)).(color.NRGBA); got == pageColor {
		t.Fatal("open inspector did not overlay the content viewport")
	}
}

func TestGraphiteKnotHasFourAlternatingCrossings(t *testing.T) {
	t.Parallel()

	canvas := image.NewRGBA(image.Rect(0, 0, 64, 64))
	center := image.Pt(32, 32)
	radius := 16
	for _, opposite := range []bool{false, true} {
		if path := knotCapsulePath(center, radius, opposite); len(path) < 18 {
			t.Fatalf("rounded knot capsule has %d sampled points, want at least 18", len(path))
		}
	}
	drawKnot(canvas, center, radius)
	crossing := knotCrossingOffset(radius)
	for _, point := range []image.Point{
		center.Add(image.Pt(0, -crossing)),
		center.Add(image.Pt(crossing, 0)),
		center.Add(image.Pt(0, crossing)),
		center.Add(image.Pt(-crossing, 0)),
	} {
		got := color.NRGBAModel.Convert(canvas.At(point.X, point.Y)).(color.NRGBA)
		if got != graphitePalette.pearl {
			t.Fatalf("knot crossing at %v = %#v, want pearl %#v", point, got, graphitePalette.pearl)
		}
	}
	for _, sample := range []struct {
		point image.Point
		want  color.NRGBA
	}{
		{center.Add(image.Pt(4, -crossing+4)), graphitePalette.teal},
		{center.Add(image.Pt(crossing+4, -4)), graphitePalette.violet},
		{center.Add(image.Pt(-4, crossing-4)), graphitePalette.teal},
		{center.Add(image.Pt(-crossing-4, 4)), graphitePalette.violet},
	} {
		got := color.NRGBAModel.Convert(canvas.At(sample.point.X, sample.point.Y)).(color.NRGBA)
		if got != sample.want {
			t.Fatalf("knot overpass at %v = %#v, want %#v", sample.point, got, sample.want)
		}
	}
}

func TestGraphiteAddressBarNormalizesAndNavigates(t *testing.T) {
	t.Parallel()

	client := shellTestLoader{document: `<html><body>navigated</body></html>`}
	page, closePage := newShellTestPage(t, client)
	defer closePage()
	shell, err := newGraphiteShell(page, ShellConfig{Loader: client})
	if err != nil {
		t.Fatal(err)
	}
	defer shell.close()
	shell.initialWindowSize(page.Frame().Viewport)
	state := inputState{}

	handled, _, _, err := shell.handleEvent(context.Background(), page, Event{
		Kind: EventKeyDown, Key: "l", Code: "KeyL", Modifiers: Modifiers{Meta: true},
	}, &state)
	if err != nil || !handled || !shell.addressFocused || !shell.selectAll {
		t.Fatalf("Meta-L handled=%t focused=%t selectAll=%t err=%v", handled, shell.addressFocused, shell.selectAll, err)
	}
	if _, _, _, err := shell.handleEvent(context.Background(), page, Event{
		Kind: EventKeyDown, Key: "n", Code: "KeyN", Text: "next.gossamer.test/path",
	}, &state); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := shell.handleEvent(context.Background(), page, Event{
		Kind: EventKeyDown, Key: "Enter", Code: "Enter",
	}, &state); err != nil {
		t.Fatal(err)
	}
	if shell.navigation == 0 {
		t.Fatal("address Enter did not start navigation")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := page.WaitNavigation(ctx, shell.navigation); err != nil {
		t.Fatal(err)
	}
	shell.syncPage(page)
	if got := page.URL().String(); got != "https://next.gossamer.test/path" {
		t.Fatalf("navigated URL = %q", got)
	}
	if shell.address != "https://next.gossamer.test/path" || shell.addressFocused || shell.navigationErr != "" {
		t.Fatalf("address state = address %q focused=%t err=%q", shell.address, shell.addressFocused, shell.navigationErr)
	}
	if !shell.canGoBack || shell.canGoForward {
		t.Fatalf("history controls after address navigation = back %t forward %t", shell.canGoBack, shell.canGoForward)
	}

	if _, _, _, err := shell.handleEvent(context.Background(), page, Event{
		Kind: EventKeyDown, Key: "[", Code: "BracketLeft", Modifiers: Modifiers{Meta: true},
	}, &state); err != nil {
		t.Fatal(err)
	}
	back := shell.navigation
	if back == 0 {
		t.Fatal("Command-[ did not start backward traversal")
	}
	if err := page.WaitNavigation(ctx, back); err != nil {
		t.Fatal(err)
	}
	shell.syncPage(page)
	if got := page.URL().String(); got != "https://start.gossamer.test/" {
		t.Fatalf("back URL = %q", got)
	}
	if shell.canGoBack || !shell.canGoForward {
		t.Fatalf("history controls after back = back %t forward %t", shell.canGoBack, shell.canGoForward)
	}

	if _, _, _, err := shell.handleEvent(context.Background(), page, Event{
		Kind: EventKeyDown, Key: "]", Code: "BracketRight", Modifiers: Modifiers{Meta: true},
	}, &state); err != nil {
		t.Fatal(err)
	}
	forward := shell.navigation
	if forward == 0 {
		t.Fatal("Command-] did not start forward traversal")
	}
	if err := page.WaitNavigation(ctx, forward); err != nil {
		t.Fatal(err)
	}
	shell.syncPage(page)
	if got := page.URL().String(); got != "https://next.gossamer.test/path" {
		t.Fatalf("forward URL = %q", got)
	}
	entriesBeforeReload, indexBeforeReload := page.History()
	if err := shell.reload(context.Background(), page); err != nil {
		t.Fatal(err)
	}
	if err := page.WaitNavigation(ctx, shell.navigation); err != nil {
		t.Fatal(err)
	}
	entriesAfterReload, indexAfterReload := page.History()
	if len(entriesAfterReload) != len(entriesBeforeReload) || indexAfterReload != indexBeforeReload {
		t.Fatalf("Graphite reload changed history length/index from %d/%d to %d/%d", len(entriesBeforeReload), indexBeforeReload, len(entriesAfterReload), indexAfterReload)
	}
}

type shellTestLoader struct {
	document string
}

func (stub shellTestLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	location, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	return &loader.Response{
		URL: location, StatusCode: 200,
		Body: io.NopCloser(strings.NewReader(stub.document)),
	}, nil
}

func newShellTestPage(t *testing.T, client shellTestLoader) (*browser.Page, func()) {
	t.Helper()
	engine := fake.New()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	page, err := browserRuntime.LoadPage(context.Background(), "https://start.gossamer.test/", client)
	if err != nil {
		_ = browserRuntime.Close()
		t.Fatal(err)
	}
	return page, func() {
		if err := browserRuntime.Close(); err != nil {
			t.Errorf("close browser runtime: %v", err)
		}
	}
}
