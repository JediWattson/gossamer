package render_test

import (
	"image/color"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func hasNonBackgroundPixelInRect(t *testing.T, img interface {
	RGBAAt(x, y int) color.RGBA
}, minX, minY, maxX, maxY int, bg color.RGBA) bool {
	t.Helper()
	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			got := img.RGBAAt(x, y)
			if got != bg {
				return true
			}
		}
	}
	return false
}

func TestFormControlTextareaAndInputRenderingAndClipping(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html><html><body style="margin:0;background:#ffffff">
		<div style="padding:8px">
			<textarea id="ta" placeholder="msg..." style="display:block;width:140px;height:40px;padding:0;border:0;font-size:16px;line-height:20px;color:#000000;background:#ffffff"></textarea>
			<input id="inp" type="text" value="WWWWWWWWWWWWWWWW" style="display:block;width:40px;height:20px;padding:0;border:0;font-size:16px;line-height:20px;color:#000000;background:#ffffff">
			<input id="pwd" type="password" value="secret" style="display:inline-block;padding:0;border:0;font-size:16px;color:#000000;background:#ffffff">
			<input id="hid" type="hidden" value="shouldnotpaint">
		</div>
	</body></html>`))
	if err != nil {
		t.Fatal(err)
	}

	frame, err := render.Render(document, render.Viewport{Width: 320, Height: 200})
	if err != nil {
		t.Fatal(err)
	}

	// Verify intrinsic non-zero inline sizing for password input and zero sizing for hidden input.
	pwdNode := findStaticPageElementByID(document, "pwd")
	pwdGeom, ok := frame.Layout.Geometry(pwdNode)
	if !ok || pwdGeom.Bounds.Width <= 20 || pwdGeom.Bounds.Height <= 10 {
		t.Fatalf("password input geometry = %+v (ok=%t), want non-zero intrinsic dimensions", pwdGeom.Bounds, ok)
	}
	hidNode := findStaticPageElementByID(document, "hid")
	if hidGeom, hasHid := frame.Layout.Geometry(hidNode); hasHid && (hidGeom.Bounds.Width != 0 || hidGeom.Bounds.Height != 0) {
		t.Fatalf("hidden input geometry = %+v, want none or zero", hidGeom.Bounds)
	}

	// Verify placeholder fragment text and muted alpha + masked password bullet text.
	foundPlaceholder := false
	foundMasked := false
	for _, cmd := range frame.DisplayList.Commands {
		if cmd.Kind != render.DrawTextCommand {
			continue
		}
		if cmd.Text == "msg..." {
			foundPlaceholder = true
			if cmd.Color.A == 0 || cmd.Color.A >= 255 {
				t.Fatalf("placeholder alpha = %d, want translucent tint", cmd.Color.A)
			}
		}
		if strings.Contains(cmd.Text, "\u2022") {
			foundMasked = true
			if strings.Contains(cmd.Text, "secret") {
				t.Fatalf("password text leaked plaintext: %q", cmd.Text)
			}
		}
	}
	if !foundPlaceholder {
		t.Fatalf("expected textarea placeholder draw command in display list")
	}
	if !foundMasked {
		t.Fatalf("expected masked password bullets in display list")
	}

	canvas, err := render.Rasterize(frame)
	if err != nil {
		t.Fatal(err)
	}
	bg := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	// Oversized input at x=8..48 must not bleed past x=50.
	if hasNonBackgroundPixelInRect(t, canvas, 52, 48, 120, 68, bg) {
		t.Fatalf("oversized input text bled past clipped bounds (x=52..120)")
	}

	// Simulate user typing/DOM value mutation updating ControlState on textarea.
	taNode := findStaticPageElementByID(document, "ta")
	taNode.Control = &dom.ControlState{Value: "line1\nline2", ValueDirty: true}
	updated, err := render.Render(document, render.Viewport{Width: 320, Height: 200})
	if err != nil {
		t.Fatal(err)
	}
	var taTexts []string
	for _, cmd := range updated.DisplayList.Commands {
		if cmd.Kind == render.DrawTextCommand && cmd.Node == taNode {
			taTexts = append(taTexts, cmd.Text)
		}
	}
	joined := strings.Join(taTexts, "|")
	if !strings.Contains(joined, "line1") || !strings.Contains(joined, "line2") || strings.Contains(joined, "msg...") {
		t.Fatalf("updated textarea draw commands = %q, want line1 and line2 without placeholder", joined)
	}
}
