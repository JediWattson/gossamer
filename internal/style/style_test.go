package style_test

import (
	"errors"
	"image/color"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/style"
)

func TestComputeReturnsTypedStylesByDOMNode(t *testing.T) {
	document := dom.NewDocument()
	html := dom.NewElement("html")
	head := dom.NewElement("head")
	styleElement := dom.NewElement("style")
	styleElement.AppendChild(dom.NewText(`
		:root { --tone: #123456; }
		body { color: #334455; }
		#target {
			display: block;
			color: var(--tone);
			width: 25%;
			border: 2px solid currentcolor;
		}
	`))
	head.AppendChild(styleElement)
	body := dom.NewElement("body")
	target := dom.NewElement("p", dom.Attribute{Name: "id", Value: "target"})
	target.AppendChild(dom.NewText("computed"))
	body.AppendChild(target)
	html.AppendChild(head)
	html.AppendChild(body)
	document.AppendChild(html)

	environment := style.Environment{
		Width:           640,
		Height:          480,
		MediaType:       "screen",
		InitialFontSize: 16,
	}
	snapshot := style.Compute(document, style.Input{Environment: environment})
	if snapshot.Root() != document {
		t.Fatal("Snapshot.Root() does not preserve DOM identity")
	}
	if got := snapshot.Environment(); got != environment {
		t.Fatalf("Snapshot.Environment() = %#v, want %#v", got, environment)
	}

	computed, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("Snapshot.Lookup(target) did not find the styled element")
	}
	if got, want := computed.Display(), style.DisplayBlock; got != want {
		t.Errorf("Display() = %v, want %v", got, want)
	}
	if got, want := computed.Color(), (color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff}); got != want {
		t.Errorf("Color() = %#v, want %#v", got, want)
	}
	if got, want := computed.Width().Unit(), style.LengthPercent; got != want {
		t.Errorf("Width().Unit() = %v, want %v", got, want)
	}
	if got, want := computed.Width().Value(), 25.0; got != want {
		t.Errorf("Width().Value() = %v, want %v", got, want)
	}
	if got, want := computed.BorderTop().Style(), style.BorderStyleSolid; got != want {
		t.Errorf("BorderTop().Style() = %v, want %v", got, want)
	}
	if _, ok := snapshot.Lookup(dom.NewElement("div")); ok {
		t.Error("Snapshot.Lookup found a node outside the computed tree")
	}
}

func TestComputeHandlesMissingRoots(t *testing.T) {
	pointerSnapshot := style.Compute(nil, style.Input{})
	if pointerSnapshot == nil {
		t.Fatal("Compute(nil) returned nil Snapshot")
	}
	if pointerSnapshot.Root() != nil {
		t.Fatal("Compute(nil) returned a non-nil root")
	}
	if _, ok := pointerSnapshot.Lookup(dom.NewElement("div")); ok {
		t.Fatal("Compute(nil) returned a populated pointer lookup")
	}

	if stableSnapshot, err := style.ComputeReadView(dom.ReadView{}, style.Input{}); !errors.Is(err, dom.ErrExpiredReadView) || stableSnapshot != nil {
		t.Fatalf("ComputeReadView(zero) = %v, %v; want nil, ErrExpiredReadView", stableSnapshot, err)
	}
}

func TestSnapshotDoesNotChangeWhenDOMChanges(t *testing.T) {
	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: "color: red"})
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)

	input := style.Input{Environment: style.Environment{Width: 320, Height: 200}}
	before := style.Compute(document, input)
	target.Attributes[0].Value = "color: blue"
	after := style.Compute(document, input)

	beforeStyle, ok := before.Lookup(target)
	if !ok {
		t.Fatal("before snapshot does not contain target")
	}
	afterStyle, ok := after.Lookup(target)
	if !ok {
		t.Fatal("after snapshot does not contain target")
	}
	if got, want := beforeStyle.Color(), (color.NRGBA{R: 0xff, A: 0xff}); got != want {
		t.Errorf("before Color() = %#v, want %#v", got, want)
	}
	if got, want := afterStyle.Color(), (color.NRGBA{B: 0xff, A: 0xff}); got != want {
		t.Errorf("after Color() = %#v, want %#v", got, want)
	}
}

func TestEnvironmentInitialFontSizeFeedsInitialAndRemValues(t *testing.T) {
	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	target := dom.NewElement("div", dom.Attribute{Name: "style", Value: "font-size: 2rem; width: 1rem"})
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)

	snapshot := style.Compute(document, style.Input{Environment: style.Environment{
		Width:           320,
		Height:          200,
		InitialFontSize: 20,
	}})
	bodyStyle, ok := snapshot.Lookup(body)
	if !ok {
		t.Fatal("snapshot does not contain body")
	}
	if got, want := bodyStyle.FontSize(), 20.0; got != want {
		t.Errorf("body FontSize() = %v, want environment initial %v", got, want)
	}
	targetStyle, ok := snapshot.Lookup(target)
	if !ok {
		t.Fatal("snapshot does not contain target")
	}
	if got, want := targetStyle.FontSize(), 40.0; got != want {
		t.Errorf("target FontSize() = %v, want %v", got, want)
	}
	if got, want := targetStyle.Width().Unit(), style.LengthPX; got != want {
		t.Fatalf("target Width().Unit() = %v, want %v", got, want)
	}
	if got, want := targetStyle.Width().Value(), 20.0; got != want {
		t.Errorf("target Width().Value() = %v, want %v", got, want)
	}
}

func TestComputeReadViewCapturesCoherentVersionAndStableIdentity(t *testing.T) {
	t.Parallel()

	root := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "color: #123456"})
	target := dom.NewElement("p")
	text := dom.NewText("stable identity")
	target.AppendChild(text)
	body.AppendChild(target)
	html.AppendChild(body)
	root.AppendChild(html)

	document, err := dom.IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	targetID := styleTestNodeID(t, document, target)
	textID := styleTestNodeID(t, document, text)
	version := document.Version()
	snapshot := styleTestSnapshot(t, document)

	if got := snapshot.Root(); got != nil {
		t.Fatalf("ID-only Snapshot.Root() = %p, want nil", got)
	}
	if got, want := snapshot.RootID(), document.RootID(); got != want {
		t.Fatalf("Snapshot.RootID() = %d, want %d", got, want)
	}
	if got := snapshot.Version(); got != version {
		t.Fatalf("Snapshot.Version() = %d, want coherent document version %d", got, version)
	}

	assertStableIDStyle(t, snapshot, target, targetID, color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
	assertStableIDStyle(t, snapshot, text, textID, color.NRGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
	if _, ok := snapshot.LookupID(dom.InvalidNodeID); ok {
		t.Fatal("Snapshot.LookupID(InvalidNodeID) unexpectedly succeeded")
	}
}

func TestComputeReadViewRejectsExpiredView(t *testing.T) {
	t.Parallel()

	root := dom.NewDocument()
	html := dom.NewElement("html")
	html.AppendChild(dom.NewElement("body"))
	root.AppendChild(html)
	document, err := dom.IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	var expired dom.ReadView
	if err := document.WithReadView(func(view dom.ReadView) error {
		expired = view
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := style.ComputeReadView(expired, style.Input{
		Environment: style.Environment{Width: 320, Height: 200},
	})
	if !errors.Is(err, dom.ErrExpiredReadView) || snapshot != nil {
		t.Fatalf("ComputeReadView(expired) = %v, %v; want nil, ErrExpiredReadView", snapshot, err)
	}
}

func TestStableIDSnapshotRemainsImmutableAcrossDetachAndReconnection(t *testing.T) {
	t.Parallel()

	root := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	redParent := dom.NewElement("section", dom.Attribute{Name: "style", Value: "color: red"})
	blueParent := dom.NewElement("section", dom.Attribute{Name: "style", Value: "color: blue"})
	child := dom.NewElement("span")
	child.AppendChild(dom.NewText("moving child"))
	redParent.AppendChild(child)
	body.AppendChild(redParent)
	body.AppendChild(blueParent)
	html.AppendChild(body)
	root.AppendChild(html)

	document, err := dom.IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	redParentID := styleTestNodeID(t, document, redParent)
	blueParentID := styleTestNodeID(t, document, blueParent)
	childID := styleTestNodeID(t, document, child)

	before := styleTestSnapshot(t, document)
	assertStableIDStyle(t, before, child, childID, color.NRGBA{R: 0xff, A: 0xff})

	if err := document.RemoveChild(redParentID, childID); err != nil {
		t.Fatal(err)
	}
	detached := styleTestSnapshot(t, document)
	if got, want := detached.Version(), before.Version()+1; got != want {
		t.Fatalf("detached Snapshot.Version() = %d, want %d", got, want)
	}
	if _, ok := detached.Lookup(child); ok {
		t.Fatal("detached snapshot retained the disconnected child pointer")
	}
	if _, ok := detached.LookupID(childID); ok {
		t.Fatal("detached snapshot retained the disconnected child NodeID")
	}
	// A later DOM mutation cannot alter a previously published immutable
	// snapshot, even when the same backing node and NodeID are retained.
	assertStableIDStyle(t, before, child, childID, color.NRGBA{R: 0xff, A: 0xff})

	if err := document.AppendNode(blueParentID, childID); err != nil {
		t.Fatal(err)
	}
	if got := styleTestNodeID(t, document, child); got != childID {
		t.Fatalf("reconnected child NodeID = %d, want preserved ID %d", got, childID)
	}
	reconnected := styleTestSnapshot(t, document)
	if got, want := reconnected.Version(), detached.Version()+1; got != want {
		t.Fatalf("reconnected Snapshot.Version() = %d, want %d", got, want)
	}
	assertStableIDStyle(t, reconnected, child, childID, color.NRGBA{B: 0xff, A: 0xff})
	assertStableIDStyle(t, before, child, childID, color.NRGBA{R: 0xff, A: 0xff})
}

func styleTestSnapshot(t *testing.T, document *dom.Document) *style.Snapshot {
	t.Helper()
	var snapshot *style.Snapshot
	if err := document.WithReadView(func(view dom.ReadView) error {
		var computeErr error
		snapshot, computeErr = style.ComputeReadView(view, style.Input{
			Environment: style.Environment{Width: 640, Height: 480, MediaType: "screen", InitialFontSize: 16},
		})
		return computeErr
	}); err != nil {
		t.Fatal(err)
	}
	if snapshot == nil {
		t.Fatal("ComputeReadView() returned nil")
	}
	return snapshot
}

func styleTestNodeID(t *testing.T, document *dom.Document, node *dom.Node) dom.NodeID {
	t.Helper()
	id, ok := document.ID(node)
	if !ok || id == dom.InvalidNodeID {
		t.Fatalf("Document.ID(%p) = %d, %t; want stable identity", node, id, ok)
	}
	return id
}

func assertStableIDStyle(
	t *testing.T,
	snapshot *style.Snapshot,
	node *dom.Node,
	id dom.NodeID,
	wantColor color.NRGBA,
) {
	t.Helper()
	if _, ok := snapshot.Lookup(node); ok {
		t.Fatalf("ID-only snapshot retained callback-scoped node pointer %p", node)
	}
	byID, idOK := snapshot.LookupID(id)
	if !idOK {
		t.Fatalf("Snapshot.LookupID(%d) did not find the connected node", id)
	}
	if got := byID.Color(); got != wantColor {
		t.Fatalf("NodeID lookup Color() = %#v, want %#v", got, wantColor)
	}
}
