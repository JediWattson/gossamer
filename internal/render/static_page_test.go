package render_test

import (
	"image/color"
	"strings"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	htmlparser "github.com/JediWattson/gossamer/internal/html"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestRenderReadableStaticPageBoxModelRegression(t *testing.T) {
	t.Parallel()

	document, err := htmlparser.Parse(strings.NewReader(`<!doctype html>
<html>
<head>
<style>
body {
	font-size: 16px;
	color: #454545;
	margin: 2em auto;
	max-width: 800px;
	padding: 1em;
	line-height: 1.4;
}
.container {
	max-width: 650px;
	margin: 20px auto;
	padding-left: 15px;
	padding-right: 15px;
}
.header {
	background-color: #42c0fd;
	color: white;
	padding: 10px 0;
	font-size: 2.2em;
	text-align: center;
}
.reasons {
	margin: 0;
	padding: 0 0 0 2em;
	text-align: left;
}
</style>
</head>
<body id="page">
<main id="article" class="container">
	<header id="banner" class="header">Always available</header>
	<p>A small, dependable page with a readable measure.</p>
	<ul id="reasons" class="reasons">
		<li id="first-reason">No script required</li>
		<li id="second-reason">Useful on every connection</li>
	</ul>
</main>
</body>
</html>`))
	if err != nil {
		t.Fatalf("html.Parse() error = %v", err)
	}

	frame, err := render.Render(document, render.Viewport{Width: 1000, Height: 900})
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	body := findStaticPageElementByID(document, "page")
	article := findStaticPageElementByID(document, "article")
	banner := findStaticPageElementByID(document, "banner")
	reasons := findStaticPageElementByID(document, "reasons")
	firstReason := findStaticPageElementByID(document, "first-reason")
	secondReason := findStaticPageElementByID(document, "second-reason")
	for name, node := range map[string]*dom.Node{
		"body": body, "article": article, "banner": banner, "reasons": reasons,
		"first reason": firstReason, "second reason": secondReason,
	} {
		if node == nil {
			t.Fatalf("%s node not found", name)
		}
	}

	bodyBox := findBox(frame.Root, body)
	articleBox := findBox(frame.Root, article)
	bannerBox := findBox(frame.Root, banner)
	reasonsBox := findBox(frame.Root, reasons)
	firstReasonBox := findBox(frame.Root, firstReason)
	secondReasonBox := findBox(frame.Root, secondReason)
	for name, box := range map[string]*render.Box{
		"body": bodyBox, "article": articleBox, "banner": bannerBox, "reasons": reasonsBox,
		"first reason": firstReasonBox, "second reason": secondReasonBox,
	} {
		if box == nil {
			t.Fatalf("%s layout box not found", name)
		}
	}

	// Best-style body sizing leaves a one-em inner gutter while the content
	// measure remains capped and centered in the wider viewport.
	assertNear(t, "body border-box x", bodyBox.Bounds.X, 84)
	assertNear(t, "body border-box width", bodyBox.Bounds.Width, 832)
	assertNear(t, "body content x", bodyBox.ContentBounds.X, 100)
	assertNear(t, "body content width", bodyBox.ContentBounds.Width, 800)
	assertNear(t, "body left padding", bodyBox.Padding.Left, 16)
	assertNear(t, "body right padding", bodyBox.Padding.Right, 16)

	// NeverSSL's narrower container keeps its 650px readable measure, centers
	// the padding box, and adds 15px gutters without shrinking the content.
	assertNear(t, "article border-box x", articleBox.Bounds.X, 160)
	assertNear(t, "article border-box width", articleBox.Bounds.Width, 680)
	assertNear(t, "article content x", articleBox.ContentBounds.X, 175)
	assertNear(t, "article content width", articleBox.ContentBounds.Width, 650)
	assertNear(t, "article left padding", articleBox.Padding.Left, 15)
	assertNear(t, "article right padding", articleBox.Padding.Right, 15)

	bannerText := findTextFragment(collectTextFragments(frame.Root), "Always available")
	if bannerText == nil {
		t.Fatal("banner text fragment not found")
	}
	assertNear(
		t,
		"centered banner text x",
		bannerText.X,
		bannerBox.ContentBounds.X+(bannerBox.ContentBounds.Width-bannerText.Width)/2,
	)
	assertNear(t, "banner content top", bannerBox.ContentBounds.Y, bannerBox.Bounds.Y+10)
	assertNear(t, "banner bottom padding", bannerBox.Bounds.Height-bannerBox.ContentBounds.Height-10, 10)

	firstBottom := firstReasonBox.Bounds.Y + firstReasonBox.Bounds.Height
	if secondReasonBox.Bounds.Y < firstBottom {
		t.Errorf("second list item y = %.2f, want at or below first bottom %.2f", secondReasonBox.Bounds.Y, firstBottom)
	}
	firstText := findTextFragment(collectTextFragments(firstReasonBox), "No script required")
	secondText := findTextFragment(collectTextFragments(secondReasonBox), "Useful on every connection")
	if firstText == nil || secondText == nil {
		t.Fatalf("list text fragments = first:%#v second:%#v, want both", firstText, secondText)
	}
	assertNear(t, "left-aligned first item", firstText.X, reasonsBox.ContentBounds.X)
	assertNear(t, "left-aligned second item", secondText.X, reasonsBox.ContentBounds.X)
	if secondText.BaselineY <= firstText.BaselineY {
		t.Errorf("list baselines = %.2f and %.2f, want increasing document order", firstText.BaselineY, secondText.BaselineY)
	}

	wantHeader := color.NRGBA{R: 0x42, G: 0xc0, B: 0xfd, A: 0xff}
	headerBackground := commandIndex(frame.DisplayList.Commands, func(command render.Command) bool {
		return command.Kind == render.FillRectCommand &&
			command.Color == wantHeader &&
			near(command.Rect.X, bannerBox.Bounds.X) &&
			near(command.Rect.Y, bannerBox.Bounds.Y) &&
			near(command.Rect.Width, bannerBox.Bounds.Width) &&
			near(command.Rect.Height, bannerBox.Bounds.Height)
	})
	if headerBackground < 0 {
		t.Error("header background does not cover its padded border box")
	}
}

func findStaticPageElementByID(node *dom.Node, id string) *dom.Node {
	if node == nil {
		return nil
	}
	if node.Type == dom.ElementNode {
		for _, attribute := range node.Attributes {
			if strings.EqualFold(attribute.Name, "id") && attribute.Value == id {
				return node
			}
		}
	}
	for _, child := range node.Children {
		if found := findStaticPageElementByID(child, id); found != nil {
			return found
		}
	}
	return nil
}
