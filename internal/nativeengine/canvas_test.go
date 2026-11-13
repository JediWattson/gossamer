package nativeengine_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/png"
	"net/url"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/nativeengine"
	"github.com/JediWattson/gossamer/internal/render"
)

func newCanvasTestPage(t *testing.T, htmlSource string) (*browser.Page, *url.URL) {
	t.Helper()
	scriptEngine := nativeengine.New(nativeengine.Config{})
	browserRuntime, err := browser.NewWithEngine(scriptEngine)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = browserRuntime.Close() })
	location, _ := url.Parse("https://gossamer.test/canvas/")
	doc := dom.NewElement("html")
	if strings.TrimSpace(htmlSource) != "" {
		parsed, err := htmlparser.Parse(strings.NewReader(htmlSource))
		if err != nil {
			t.Fatal(err)
		}
		doc = parsed
	} else {
		body := dom.NewElement("body")
		doc.AppendChild(body)
	}
	page, err := browserRuntime.NewPage(doc, location)
	if err != nil {
		t.Fatal(err)
	}
	return page, location
}

func TestNativeCanvasAPIPropertiesTransformsAndToDataURL(t *testing.T) {
	t.Parallel()

	page, location := newCanvasTestPage(t, `<!doctype html><html><body style="margin:0"><canvas id="c"></canvas></body></html>`)
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
		const canvas = document.getElementById("c");
		if (!(canvas instanceof HTMLCanvasElement) || !(canvas instanceof HTMLElement)) {
			throw new Error("canvas prototype chain mismatch");
		}
		if (canvas.width !== 300 || canvas.height !== 150) {
			throw new Error("unexpected default dimensions: " + canvas.width + "x" + canvas.height);
		}
		if (canvas.getContext("webgl") !== null) {
			throw new Error("expected null for unsupported webgl context");
		}
		const ctx = canvas.getContext("2d");
		if (!(ctx instanceof CanvasRenderingContext2D) || ctx.canvas !== canvas) {
			throw new Error("invalid CanvasRenderingContext2D");
		}
		if (canvas.getContext("2d") !== ctx) {
			throw new Error("getContext('2d') must return same context object");
		}

		// Resize resets both bitmap and context state.
		canvas.width = 40;
		canvas.height = 30;
		ctx.fillStyle = "rgb(255, 0, 0)";
		if (ctx.fillStyle !== "#ff0000") {
			throw new Error("unexpected serialized fillStyle: " + ctx.fillStyle);
		}
		ctx.lineWidth = 5;
		ctx.save();
		ctx.translate(10, 10);
		canvas.width = 40; // Setting same numeric width resets bitmap and context state.
		if (ctx.fillStyle !== "#000000" || ctx.lineWidth !== 1) {
			throw new Error("resize did not reset context state");
		}

		// Draw red box via save/translate/restore + clear punchout.
		ctx.save();
		ctx.translate(5, 5);
		ctx.fillStyle = "#ff0000";
		ctx.fillRect(0, 0, 20, 20);
		ctx.restore();
		ctx.clearRect(10, 10, 4, 4);

		// Expose data URL globally for Go PNG verification.
		globalThis.__pngUrl = canvas.toDataURL();

		// 0x0 canvas returns "data:,".
		const zero = document.createElement("canvas");
		zero.width = 0;
		zero.height = 0;
		if (zero.toDataURL() !== "data:,") {
			throw new Error("expected data:, for empty canvas, got " + zero.toDataURL());
		}

		// Negative width falls back cleanly.
		zero.width = -5;
		if (zero.width !== 300) {
			throw new Error("expected negative width fallback 300, got " + zero.width);
		}
	`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}

	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
		if (!globalThis.__pngUrl.startsWith("data:image/png;base64,")) {
			throw new Error("unexpected dataURL prefix: " + globalThis.__pngUrl);
		}
	`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Render and verify pixel values directly on painted frame.
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	frame := page.Frame()
	raster, err := render.RasterizeAtScale(frame, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Pixel at (7, 7) is inside the translated [5..25, 5..25] red rect.
	redPx := raster.RGBAAt(7, 7)
	if redPx.R < 250 || redPx.G != 0 || redPx.B != 0 || redPx.A != 255 {
		t.Fatalf("canvas rendered pixel at (7,7) = %+v, want red", redPx)
	}
	// Pixel at (12, 12) was cleared by clearRect(10, 10, 4, 4).
	clearedPx := raster.RGBAAt(12, 12)
	if clearedPx.R != 255 || clearedPx.G != 255 || clearedPx.B != 255 {
		t.Fatalf("cleared pixel at (12,12) = %+v, want page white background", clearedPx)
	}
}

func TestNativeCanvasPathsStrokesImageDataDrawImageAndRepaint(t *testing.T) {
	t.Parallel()

	page, location := newCanvasTestPage(t, `<!doctype html><html><body style="margin:0"><canvas id="c" width="64" height="64"></canvas></body></html>`)
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
		const canvas = document.getElementById("c");
		const ctx = canvas.getContext("2d");

		// Zero-length dot with round cap should paint circle at (10, 10).
		ctx.strokeStyle = "#0000ff";
		ctx.lineWidth = 8;
		ctx.lineCap = "round";
		ctx.lineJoin = "round";
		ctx.beginPath();
		ctx.moveTo(10, 10);
		ctx.lineTo(10, 10);
		ctx.stroke();

		// Semi-transparent 2-segment corner stroke must not double-darken at joint (30, 20).
		ctx.save();
		ctx.globalAlpha = 0.5;
		ctx.strokeStyle = "#00ff00";
		ctx.lineWidth = 10;
		ctx.beginPath();
		ctx.moveTo(20, 20);
		ctx.lineTo(35, 20);
		ctx.lineTo(35, 35);
		ctx.stroke();
		ctx.restore();

		const dot = ctx.getImageData(10, 10, 1, 1);
		if (dot.width !== 1 || dot.height !== 1 || !(dot.data instanceof Uint8ClampedArray)) {
			throw new Error("unexpected getImageData result");
		}
		if (dot.data[2] !== 255 || dot.data[3] !== 255) {
			throw new Error("round zero-length dot not painted blue: " + Array.from(dot.data));
		}

		const joint = ctx.getImageData(35, 20, 1, 1);
		if (joint.data[3] < 120 || joint.data[3] > 135) {
			throw new Error("expected ~128 alpha at semi-transparent stroke joint (no double blend), got " + joint.data[3]);
		}

		// Arc fill + destination-out erase.
		ctx.fillStyle = "#ff00ff";
		ctx.beginPath();
		ctx.arc(50, 50, 10, 0, Math.PI * 2);
		ctx.fill();
		const beforeErase = ctx.getImageData(50, 50, 1, 1);
		if (beforeErase.data[0] !== 255 || beforeErase.data[2] !== 255) {
			throw new Error("filled circle center mismatch: " + Array.from(beforeErase.data));
		}
		ctx.globalCompositeOperation = "destination-out";
		ctx.fillStyle = "#000000";
		ctx.beginPath();
		ctx.arc(50, 50, 4, 0, Math.PI * 2);
		ctx.fill();
		ctx.globalCompositeOperation = "source-over";
		const afterErase = ctx.getImageData(50, 50, 1, 1);
		if (afterErase.data[3] !== 0) {
			throw new Error("destination-out did not erase center: alpha=" + afterErase.data[3]);
		}

		// putImageData + ImageData constructor + drawImage(otherCanvas).
		const custom = new ImageData(new Uint8ClampedArray([12, 34, 56, 255, 12, 34, 56, 255, 12, 34, 56, 255, 12, 34, 56, 255]), 2, 2);
		const copyCanvas = document.createElement("canvas");
		copyCanvas.width = 4;
		copyCanvas.height = 4;
		const cctx = copyCanvas.getContext("2d");
		cctx.putImageData(custom, 1, 1);
		ctx.drawImage(copyCanvas, 1, 1, 2, 2, 0, 50, 4, 4);
		const copied = ctx.getImageData(2, 52, 1, 1);
		if (copied.data[0] !== 12 || copied.data[1] !== 34 || copied.data[2] !== 56 || copied.data[3] !== 255) {
			throw new Error("drawImage(canvas) mismatch: " + Array.from(copied.data));
		}

		// Quadratic & cubic curves + measureText.
		ctx.beginPath();
		ctx.moveTo(0, 40);
		ctx.quadraticCurveTo(10, 30, 20, 40);
		ctx.bezierCurveTo(25, 45, 30, 35, 40, 40);
		ctx.stroke();
		if (!(ctx.measureText("hello").width > 0)) {
			throw new Error("measureText width invalid");
		}
	`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Task 1's autoRender queued a render task; drain it before queueing next script.
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Render frame before dynamic mutation, then mutate and verify Dirty() + updated frame pixels.
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	if page.Dirty() {
		t.Fatal("expected clean page right after Render")
	}

	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
		(() => {
			const c = document.getElementById("c");
			const g = c.getContext("2d");
			g.fillStyle = "#112233";
			g.fillRect(0, 0, 8, 8);
			globalThis.__encoded = c.toDataURL("image/png");
		})();
	`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !page.Dirty() {
		t.Fatal("expected page.Dirty() == true after canvas mutation")
	}

	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	frame2 := page.Frame()
	raster2, err := render.RasterizeAtScale(frame2, 1)
	if err != nil {
		t.Fatal(err)
	}
	px := raster2.RGBAAt(4, 4)
	if px.R != 0x11 || px.G != 0x22 || px.B != 0x33 || px.A != 0xff {
		t.Fatalf("repainted pixel at (4,4) = %+v, want #112233", px)
	}

	// Drain previous auto-render task, then store toDataURL on body attribute and decode PNG.
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
		document.body.setAttribute("data-url", globalThis.__encoded);
	`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Read data-url via render frame root child (body).
	if err := page.Render(); err != nil {
		t.Fatal(err)
	}
	frame3 := page.Frame()
	bodyAttr := ""
	var findBody func(*dom.Node)
	findBody = func(n *dom.Node) {
		if n == nil {
			return
		}
		if n.Type == dom.ElementNode && n.Data == "body" {
			for _, a := range n.Attributes {
				if a.Name == "data-url" {
					bodyAttr = a.Value
				}
			}
		}
		for _, c := range n.Children {
			findBody(c)
		}
	}
	findBody(frame3.Root.Node)
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(bodyAttr, prefix) {
		t.Fatalf("dataURL missing PNG prefix: %q", bodyAttr)
	}
	rawPNG, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(bodyAttr, prefix))
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}
	decodedImg, err := png.Decode(bytes.NewReader(rawPNG))
	if err != nil {
		t.Fatalf("png.Decode() error = %v", err)
	}
	decodedPx := decodedImg.At(4, 4)
	r, g, b, a := decodedPx.RGBA()
	if uint8(r>>8) != 0x11 || uint8(g>>8) != 0x22 || uint8(b>>8) != 0x33 || uint8(a>>8) != 0xff {
		t.Fatalf("decoded PNG pixel at (4,4) = (%d,%d,%d,%d), want (17,34,51,255)", r>>8, g>>8, b>>8, a>>8)
	}
}
