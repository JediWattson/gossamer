package render_test

import (
	"strings"
	"testing"

	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestInlineSVGStrokePathCurrentColorAndFlexCentering(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0;background:#000000">
		<div style="display:flex;align-items:center;justify-content:center;width:48px;height:48px;color:#00ff00">
			<svg id="arrow" style="width:24px;height:24px" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
				<defs><rect x="0" y="0" width="24" height="24" fill="#ff0000"/></defs>
				<path d="M12 19V5M5 12l7-7 7 7"/>
			</svg>
		</div>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 48, Height: 48})
	if err != nil {
		t.Fatal(err)
	}
	svgNode := findStaticPageElementByID(document, "arrow")
	geom, ok := frame.Layout.Geometry(svgNode)
	if !ok {
		t.Fatal("missing svg geometry")
	}
	if geom.Bounds.X != 12 || geom.Bounds.Y != 12 || geom.Bounds.Width != 24 || geom.Bounds.Height != 24 {
		t.Fatalf("unexpected svg geometry: %+v", geom.Bounds)
	}

	canvas, err := render.Rasterize(frame)
	if err != nil {
		t.Fatal(err)
	}
	// Outer background must stay black (defs rect must not paint).
	if corner := canvas.RGBAAt(1, 1); corner.R != 0 || corner.G != 0 || corner.B != 0 {
		t.Fatalf("expected black corner outside svg, got %+v", corner)
	}
	// Vertical arrow shaft center at (24, 24) and join peak (24, 17) must paint solid green stroke.
	if shaft := canvas.RGBAAt(24, 24); shaft.G < 220 || shaft.R > 20 || shaft.B > 20 {
		t.Fatalf("expected bright green stroke on shaft at (24,24), got %+v", shaft)
	}
	if apex := canvas.RGBAAt(24, 17); apex.G < 180 || apex.R > 20 {
		t.Fatalf("expected green stroke at arrow apex (24,17), got %+v", apex)
	}
}

func TestInlineSVGPrimitivesTransformsAndIntrinsicSizing(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0;background:#ffffff">
		<svg id="shapes" width="40" height="40" viewBox="0 0 40 40">
			<circle cx="10" cy="10" r="6" fill="#ff0000"/>
			<g transform="translate(20, 4)">
				<rect x="0" y="0" width="12" height="12" rx="2" fill="#0000ff"/>
			</g>
			<line x1="4" y1="30" x2="36" y2="30" stroke="#00aa00" stroke-width="4" stroke-linecap="round"/>
			<path d="M8 22 A 4 4 0 1 0 16 22 Z" style="fill: #aa00aa"/>
		</svg>
		<svg id="ratio" style="display:block;width:40px" viewBox="0 0 100 50">
			<polygon points="0,0 100,0 50,50" fill="#112233"/>
		</svg>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	frame, err := render.Render(document, render.Viewport{Width: 100, Height: 100})
	if err != nil {
		t.Fatal(err)
	}
	shapesGeom, ok := frame.Layout.Geometry(findStaticPageElementByID(document, "shapes"))
	if !ok || shapesGeom.Bounds.Width != 40 || shapesGeom.Bounds.Height != 40 {
		t.Fatalf("shapes attr size = %+v (ok=%t), want 40x40", shapesGeom.Bounds, ok)
	}
	ratioGeom, ok := frame.Layout.Geometry(findStaticPageElementByID(document, "ratio"))
	if !ok || ratioGeom.Bounds.Width != 40 || ratioGeom.Bounds.Height != 20 {
		t.Fatalf("viewBox ratio size = %+v (ok=%t), want 40x20", ratioGeom.Bounds, ok)
	}

	canvas, err := render.Rasterize(frame)
	if err != nil {
		t.Fatal(err)
	}
	if got := canvas.RGBAAt(10, 10); got.R < 220 || got.G > 20 || got.B > 20 {
		t.Fatalf("circle center (10,10) = %+v, want red", got)
	}
	if got := canvas.RGBAAt(26, 10); got.B < 220 || got.R > 20 || got.G > 20 {
		t.Fatalf("translated rect center (26,10) = %+v, want blue", got)
	}
	if got := canvas.RGBAAt(20, 30); got.G < 140 || got.R > 20 {
		t.Fatalf("line center (20,30) = %+v, want green", got)
	}
	if got := canvas.RGBAAt(20, 48); got.R > 40 || got.G > 50 || got.B > 70 {
		t.Fatalf("polygon interior (20,48) = %+v, want dark #112233", got)
	}
}
