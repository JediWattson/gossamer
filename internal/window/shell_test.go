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
