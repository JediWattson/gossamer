package render_test

import (
	"image/color"
	"strings"
	"testing"

	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestRenderGNUSolidBorderGeometryAndPaint(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html>
<html>
<head>
<style>
html { background: #e4e4e4; }
body {
	font-size: 1em;
	max-width: 74.92em;
	padding: 0;
	margin: 0 auto;
	background: white;
	border: .1em solid #bbb;
	border-top: 0;
}
#navigation {
	background: #a32d2a;
	border-top: .0625em solid #a32d2a;
	border-bottom: .0625em solid #a32d2a;
}
.announcement {
	border-left: .4em solid #5c5;
	padding: .4em 1em;
	margin: 0;
}
</style>
</head>
<body id="page">
	<nav id="navigation">GNU navigation</nav>
	<aside id="callout" class="announcement">Free software matters</aside>
</body>
</html>`))
	if err != nil {
		t.Fatalf("html.Parse() error = %v", err)
	}

	frame, err := render.Render(document, render.Viewport{Width: 400, Height: 180})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	body := findStaticPageElementByID(document, "page")
	navigation := findStaticPageElementByID(document, "navigation")
	callout := findStaticPageElementByID(document, "callout")
	bodyBox := findBox(frame.Root, body)
	navigationBox := findBox(frame.Root, navigation)
	calloutBox := findBox(frame.Root, callout)
	if bodyBox == nil || navigationBox == nil || calloutBox == nil {
		t.Fatalf("layout boxes = body:%#v navigation:%#v callout:%#v, want all", bodyBox, navigationBox, calloutBox)
	}

	// The body has 1.6px left, right, and bottom borders. border-top: 0
	// removes both its geometry and paint while leaving the border box at the
	// viewport edge.
	assertNear(t, "body border-box x", bodyBox.Bounds.X, 0)
	assertNear(t, "body border-box width", bodyBox.Bounds.Width, 400)
	assertNear(t, "body content x", bodyBox.ContentBounds.X, 1.6)
	assertNear(t, "body content y", bodyBox.ContentBounds.Y, bodyBox.Bounds.Y)
	assertNear(t, "body content width", bodyBox.ContentBounds.Width, 396.8)
	assertNear(t, "body bottom inset", bodyBox.Bounds.Height-bodyBox.ContentBounds.Height, 1.6)

	gray := color.NRGBA{R: 0xbb, G: 0xbb, B: 0xbb, A: 0xff}
	assertGNUFillRect(t, "body left border", frame.DisplayList.Commands, gray, render.Rect{
		X: bodyBox.Bounds.X, Y: bodyBox.Bounds.Y, Width: 1.6, Height: bodyBox.Bounds.Height,
	})
	assertGNUFillRect(t, "body right border", frame.DisplayList.Commands, gray, render.Rect{
		X: bodyBox.Bounds.X + bodyBox.Bounds.Width - 1.6,
		Y: bodyBox.Bounds.Y, Width: 1.6, Height: bodyBox.Bounds.Height,
	})
	assertGNUFillRect(t, "body bottom border", frame.DisplayList.Commands, gray, render.Rect{
		X: bodyBox.Bounds.X, Y: bodyBox.Bounds.Y + bodyBox.Bounds.Height - 1.6,
		Width: bodyBox.Bounds.Width, Height: 1.6,
	})
	bodyTopBorder := gnuFillRectIndex(frame.DisplayList.Commands, gray, render.Rect{
		X: bodyBox.Bounds.X, Y: bodyBox.Bounds.Y, Width: bodyBox.Bounds.Width, Height: 1.6,
	})
	if bodyTopBorder >= 0 {
		t.Errorf("body top border painted at command %d despite border-top: 0", bodyTopBorder)
	}

	// GNU uses same-color one-pixel borders around its navigation background.
	assertNear(t, "navigation x", navigationBox.Bounds.X, bodyBox.ContentBounds.X)
	assertNear(t, "navigation width", navigationBox.Bounds.Width, bodyBox.ContentBounds.Width)
	assertNear(t, "navigation top border inset", navigationBox.ContentBounds.Y-navigationBox.Bounds.Y, 1)
	assertNear(
		t,
		"navigation bottom border inset",
		navigationBox.Bounds.Y+navigationBox.Bounds.Height-navigationBox.ContentBounds.Y-navigationBox.ContentBounds.Height,
		1,
	)
	navigationRed := color.NRGBA{R: 0xa3, G: 0x2d, B: 0x2a, A: 0xff}
	assertGNUFillRect(t, "navigation background", frame.DisplayList.Commands, navigationRed, navigationBox.Bounds)
	assertGNUFillRect(t, "navigation top border", frame.DisplayList.Commands, navigationRed, render.Rect{
		X: navigationBox.Bounds.X, Y: navigationBox.Bounds.Y, Width: navigationBox.Bounds.Width, Height: 1,
	})
	assertGNUFillRect(t, "navigation bottom border", frame.DisplayList.Commands, navigationRed, render.Rect{
		X: navigationBox.Bounds.X, Y: navigationBox.Bounds.Y + navigationBox.Bounds.Height - 1,
		Width: navigationBox.Bounds.Width, Height: 1,
	})

	// The callout's 6.4px accent participates in width calculation before its
	// 16px horizontal padding, and is painted for the full border-box height.
	assertNear(t, "callout x", calloutBox.Bounds.X, bodyBox.ContentBounds.X)
	assertNear(t, "callout width", calloutBox.Bounds.Width, bodyBox.ContentBounds.Width)
	assertNear(t, "callout content x", calloutBox.ContentBounds.X, calloutBox.Bounds.X+6.4+16)
	assertNear(t, "callout content width", calloutBox.ContentBounds.Width, calloutBox.Bounds.Width-6.4-32)
	assertNear(t, "callout content top", calloutBox.ContentBounds.Y, calloutBox.Bounds.Y+6.4)
	assertNear(
		t,
		"callout bottom padding",
		calloutBox.Bounds.Y+calloutBox.Bounds.Height-calloutBox.ContentBounds.Y-calloutBox.ContentBounds.Height,
		6.4,
	)
	green := color.NRGBA{R: 0x55, G: 0xcc, B: 0x55, A: 0xff}
	assertGNUFillRect(t, "callout left border", frame.DisplayList.Commands, green, render.Rect{
		X: calloutBox.Bounds.X, Y: calloutBox.Bounds.Y, Width: 6.4, Height: calloutBox.Bounds.Height,
	})
	calloutText := findTextFragment(collectTextFragments(frame.Root), "Free software matters")
	if calloutText == nil {
		t.Fatal("callout text fragment not found")
	}
	assertNear(t, "callout text x", calloutText.X, calloutBox.ContentBounds.X)
}

func assertGNUFillRect(t *testing.T, name string, commands []render.Command, wantColor color.NRGBA, wantRect render.Rect) {
	t.Helper()
	if index := gnuFillRectIndex(commands, wantColor, wantRect); index < 0 {
		t.Errorf("%s fill not found: color=%#v rect=%#v", name, wantColor, wantRect)
	}
}

func gnuFillRectIndex(commands []render.Command, wantColor color.NRGBA, wantRect render.Rect) int {
	return commandIndex(commands, func(command render.Command) bool {
		return command.Kind == render.FillRectCommand &&
			command.Color == wantColor &&
			near(command.Rect.X, wantRect.X) &&
			near(command.Rect.Y, wantRect.Y) &&
			near(command.Rect.Width, wantRect.Width) &&
			near(command.Rect.Height, wantRect.Height)
	})
}
