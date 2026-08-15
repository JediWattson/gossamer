package render_test

import (
	"errors"
	"image"
	"image/color"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/render"
)

func TestRenderWithStyleSnapshotUsesTheExactImmutableSnapshot(t *testing.T) {
	t.Parallel()

	document := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin: 0"})
	target := dom.NewElement("p", dom.Attribute{Name: "style", Value: "color: red"})
	target.AppendChild(dom.NewText("snapshot color"))
	body.AppendChild(target)
	html.AppendChild(body)
	document.AppendChild(html)

	viewport := render.Viewport{Width: 320, Height: 200}
	redSnapshot, err := render.ComputeStyleSnapshot(document, viewport, render.Resources{})
	if err != nil {
		t.Fatalf("ComputeStyleSnapshot(red) error = %v", err)
	}
	target.Attributes[0].Value = "color: blue"

	redFrame, err := render.RenderWithStyleSnapshot(document, viewport, render.Resources{}, redSnapshot)
	if err != nil {
		t.Fatalf("RenderWithStyleSnapshot(red) error = %v", err)
	}
	if redFrame.ComputedStyles != redSnapshot {
		t.Fatalf("Frame.ComputedStyles = %p, want exact supplied snapshot %p", redFrame.ComputedStyles, redSnapshot)
	}
	assertSnapshotTextColor(t, redFrame, "snapshot color", color.NRGBA{R: 0xff, A: 0xff})

	blueSnapshot, err := render.ComputeStyleSnapshot(document, viewport, render.Resources{})
	if err != nil {
		t.Fatalf("ComputeStyleSnapshot(blue) error = %v", err)
	}
	if blueSnapshot == redSnapshot {
		t.Fatal("recomputed style snapshot reused the stale snapshot pointer")
	}
	blueFrame, err := render.RenderWithStyleSnapshot(document, viewport, render.Resources{}, blueSnapshot)
	if err != nil {
		t.Fatalf("RenderWithStyleSnapshot(blue) error = %v", err)
	}
	if blueFrame.ComputedStyles != blueSnapshot {
		t.Fatalf("Frame.ComputedStyles = %p, want exact recomputed snapshot %p", blueFrame.ComputedStyles, blueSnapshot)
	}
	assertSnapshotTextColor(t, blueFrame, "snapshot color", color.NRGBA{B: 0xff, A: 0xff})
	// Rendering the old snapshot again must remain red after both the DOM
	// mutation and the later style computation.
	redAgain, err := render.RenderWithStyleSnapshot(document, viewport, render.Resources{}, redSnapshot)
	if err != nil {
		t.Fatalf("RenderWithStyleSnapshot(red again) error = %v", err)
	}
	assertSnapshotTextColor(t, redAgain, "snapshot color", color.NRGBA{R: 0xff, A: 0xff})
}

func TestRenderReadViewRejectsStaleStyleSnapshotVersion(t *testing.T) {
	t.Parallel()

	document, targetID := indexedSnapshotDocument(t)
	viewport := render.Viewport{Width: 320, Height: 200}
	snapshot, err := render.ComputeDocumentStyleSnapshot(document, viewport, render.Resources{})
	if err != nil {
		t.Fatalf("ComputeDocumentStyleSnapshot() error = %v", err)
	}
	if err := document.SetAttribute(targetID, "style", "color: blue"); err != nil {
		t.Fatal(err)
	}

	err = document.WithReadView(func(view dom.ReadView) error {
		_, renderErr := render.RenderReadViewWithStyleSnapshot(view, viewport, render.Resources{}, snapshot)
		return renderErr
	})
	if err == nil || !strings.Contains(err.Error(), "style snapshot version") {
		t.Fatalf("RenderReadViewWithStyleSnapshot(stale) error = %v, want version mismatch", err)
	}
}

func TestRenderReadViewRejectsPointerBasedStyleSnapshot(t *testing.T) {
	t.Parallel()

	document, _ := indexedSnapshotDocument(t)
	viewport := render.Viewport{Width: 320, Height: 200}
	pointerSnapshot, err := render.ComputeStyleSnapshot(document.Root(), viewport, render.Resources{})
	if err != nil {
		t.Fatalf("ComputeStyleSnapshot() error = %v", err)
	}

	err = document.WithReadView(func(view dom.ReadView) error {
		_, renderErr := render.RenderReadViewWithStyleSnapshot(view, viewport, render.Resources{}, pointerSnapshot)
		return renderErr
	})
	if err == nil || !strings.Contains(err.Error(), "different document") {
		t.Fatalf("RenderReadViewWithStyleSnapshot(pointer snapshot) error = %v, want document mismatch", err)
	}
}

func TestRenderReadViewRejectsSnapshotFromDifferentDocument(t *testing.T) {
	t.Parallel()

	first, _ := indexedSnapshotDocument(t)
	second, _ := indexedSnapshotDocument(t)
	if first.RootID() != second.RootID() || first.Version() != second.Version() {
		t.Fatal("test documents do not exercise colliding root IDs and versions")
	}
	viewport := render.Viewport{Width: 320, Height: 200}
	snapshot, err := render.ComputeDocumentStyleSnapshot(first, viewport, render.Resources{})
	if err != nil {
		t.Fatalf("ComputeDocumentStyleSnapshot(first) error = %v", err)
	}

	err = second.WithReadView(func(view dom.ReadView) error {
		_, renderErr := render.RenderReadViewWithStyleSnapshot(view, viewport, render.Resources{}, snapshot)
		return renderErr
	})
	if err == nil || !strings.Contains(err.Error(), "different document") {
		t.Fatalf("RenderReadViewWithStyleSnapshot(cross-document) error = %v, want document mismatch", err)
	}
}

func TestReadViewAwareRenderAPIsRejectExpiredView(t *testing.T) {
	t.Parallel()

	document, _ := indexedSnapshotDocument(t)
	viewport := render.Viewport{Width: 320, Height: 200}
	snapshot, err := render.ComputeDocumentStyleSnapshot(document, viewport, render.Resources{})
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

	computed, err := render.ComputeStyleSnapshotFromReadView(expired, viewport, render.Resources{})
	if !errors.Is(err, dom.ErrExpiredReadView) || computed != nil {
		t.Fatalf("ComputeStyleSnapshotFromReadView(expired) = %v, %v; want nil, ErrExpiredReadView", computed, err)
	}
	frame, err := render.RenderReadViewWithStyleSnapshot(expired, viewport, render.Resources{}, snapshot)
	if !errors.Is(err, dom.ErrExpiredReadView) || frame != nil {
		t.Fatalf("RenderReadViewWithStyleSnapshot(expired) = %v, %v; want nil, ErrExpiredReadView", frame, err)
	}
}

func TestRenderReadAccessKeepsDocumentLockedThroughLayout(t *testing.T) {
	t.Parallel()

	root := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin: 0"})
	imageElement := dom.NewElement("img")
	body.AppendChild(imageElement)
	html.AppendChild(body)
	root.AppendChild(html)
	document, err := dom.IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	imageID, ok := document.ID(imageElement)
	if !ok {
		t.Fatal("image element has no stable ID")
	}
	viewport := render.Viewport{Width: 320, Height: 200}
	snapshot, err := render.ComputeDocumentStyleSnapshot(document, viewport, render.Resources{})
	if err != nil {
		t.Fatal(err)
	}
	blocking := &blockingSnapshotImage{
		entered: make(chan struct{}),
		release: make(chan struct{}),
	}
	resources := render.Resources{Images: map[*dom.Node]image.Image{imageElement: blocking}}

	renderDone := make(chan error, 1)
	callbackReturning := make(chan struct{})
	readDone := make(chan error, 1)
	go func() {
		readDone <- document.WithReadView(func(view dom.ReadView) error {
			go func() {
				_, renderErr := render.RenderReadViewWithStyleSnapshot(view, viewport, resources, snapshot)
				renderDone <- renderErr
			}()
			select {
			case <-blocking.entered:
				close(callbackReturning)
				return nil
			case <-time.After(time.Second):
				return errors.New("render did not reach image layout")
			}
		})
	}()
	<-callbackReturning

	writerStarted := make(chan struct{})
	writerDone := make(chan error, 1)
	go func() {
		close(writerStarted)
		writerDone <- document.SetAttribute(imageID, "alt", "updated")
	}()
	<-writerStarted
	select {
	case err := <-readDone:
		t.Fatalf("WithReadView returned while render layout held an access: %v", err)
	default:
	}
	select {
	case err := <-writerDone:
		t.Fatalf("writer completed while render layout held an access: %v", err)
	default:
	}

	close(blocking.release)
	select {
	case err := <-renderDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("render remained blocked after releasing image layout")
	}
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("WithReadView remained blocked after render completed")
	}
	select {
	case err := <-writerDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("writer remained blocked after render completed")
	}
}

type blockingSnapshotImage struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (blocking *blockingSnapshotImage) ColorModel() color.Model { return color.RGBAModel }

func (blocking *blockingSnapshotImage) Bounds() image.Rectangle {
	blocking.once.Do(func() { close(blocking.entered) })
	<-blocking.release
	return image.Rect(0, 0, 16, 16)
}

func (blocking *blockingSnapshotImage) At(int, int) color.Color {
	return color.RGBA{A: 0xff}
}

func indexedSnapshotDocument(t *testing.T) (*dom.Document, dom.NodeID) {
	t.Helper()
	root := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body", dom.Attribute{Name: "style", Value: "margin: 0"})
	target := dom.NewElement("p", dom.Attribute{Name: "style", Value: "color: red"})
	target.AppendChild(dom.NewText("indexed snapshot"))
	body.AppendChild(target)
	html.AppendChild(body)
	root.AppendChild(html)

	document, err := dom.IndexDocument(root)
	if err != nil {
		t.Fatal(err)
	}
	targetID, ok := document.ID(target)
	if !ok {
		t.Fatal("indexed target has no stable ID")
	}
	return document, targetID
}

func assertSnapshotTextColor(t *testing.T, frame *render.Frame, text string, want color.NRGBA) {
	t.Helper()
	if frame == nil {
		t.Fatal("render frame is nil")
	}
	for _, command := range frame.DisplayList.Commands {
		if command.Kind == render.DrawTextCommand && strings.Contains(command.Text, text) {
			if command.Color != want {
				t.Fatalf("display-list text color = %#v, want %#v", command.Color, want)
			}
			return
		}
	}
	t.Fatalf("display-list text command containing %q not found", text)
}
