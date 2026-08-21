package engineparity

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/loader"
	"github.com/JediWattson/gossamer/internal/nativeengine"
	"github.com/JediWattson/gossamer/internal/render"
	"github.com/JediWattson/gossamer/internal/sitecompat"
)

const fidelityPaintSHA256 = "7cf7f92f92c73be1ac79bb97e05e6a546757f10b3cb414548555b5e9d0fad656"

func TestStrandRenderingFidelityBaseline(t *testing.T) {
	runRenderingFidelityBaseline(t, nativeengine.New(nativeengine.Config{}))
}

func runRenderingFidelityBaseline(t *testing.T, engine browser.Engine) {
	t.Helper()
	report, err := sitecompat.Run(context.Background(), engine, "https://parity.gossamer.test/fidelity.html", fidelityLoader{}, sitecompat.Options{
		EngineName: "parity", DOMLimit: 30, Viewport: render.Viewport{Width: 640, Height: 360},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || report.Frame == nil || report.Frame.Commands < 10 || report.Frame.PaintedBounds == nil {
		t.Fatalf("rendering fidelity report = %#v", report)
	}
	if report.Frame.PaintSHA256 != fidelityPaintSHA256 {
		t.Fatalf("rendering fidelity SHA-256 = %s, want %s", report.Frame.PaintSHA256, fidelityPaintSHA256)
	}
}

type fidelityLoader struct{}

func (fidelityLoader) Load(_ context.Context, rawURL string) (*loader.Response, error) {
	location, _ := url.Parse(rawURL)
	return &loader.Response{
		URL: location, StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/html"}},
		Body: io.NopCloser(strings.NewReader(`<!doctype html><html><head><style>
html, body { margin: 0; width: 100%; height: 100%; background: #10131a; color: #eef2ff; font: 16px/24px sans-serif; }
#app { display: grid; grid-template-columns: 168px 1fr; width: 100%; height: 360px; }
aside { background: #1a2030; padding: 24px 16px; border-right: 2px solid #303a52; }
aside strong { display: block; font-size: 22px; margin-bottom: 20px; color: #92b4ff; }
main { display: flex; align-items: center; justify-content: center; padding: 24px; }
.card { position: relative; box-sizing: border-box; width: 320px; height: 168px; padding: 28px; background: #283148; border: 3px solid #4c5e85; }
.card.live { background: #243d36; border-color: #5cc99a; }
.badge { position: absolute; right: 12px; top: 12px; padding: 2px 8px; background: #ffcf66; color: #302300; font-size: 12px; }
#status { display: block; margin-top: 24px; color: #b9c6e4; }
@media (max-width: 500px) { #app { grid-template-columns: 1fr; } aside { display: none; } }
</style></head><body><div id="app"><aside><strong>Strand</strong><span>visual gate</span></aside><main><section id="card" class="card"><div class="badge">LIVE</div><b>Engine-neutral pixels</b><span id="status">booting</span></section></main></div><script>
document.getElementById("card").className = "card live";
document.getElementById("status").textContent = "ready";
</script></body></html>`)),
	}, nil
}
