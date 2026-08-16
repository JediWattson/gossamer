package browser

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

func TestIframeChildPageOwnsIndependentDocumentRealm(t *testing.T) {
	t.Parallel()

	browser, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	parentRoot := dom.NewDocument()
	iframe := dom.NewElement("iframe", dom.Attribute{Name: "id", Value: "child"})
	parentRoot.AppendChild(iframe)
	location, _ := url.Parse("https://parent.test/")
	parent, err := browser.NewPage(parentRoot, location)
	if err != nil {
		t.Fatal(err)
	}
	iframeID, _ := parent.Document().ID(iframe)
	iframeHandle := NodeHandle{Document: parent.DocumentGeneration(), Node: iframeID}
	childRoot := dom.NewDocument()
	childRoot.AppendChild(dom.NewElement("p"))
	childLocation, _ := url.Parse("https://child.test/")
	child, err := parent.AttachChildFrame(iframeHandle, childRoot, childLocation)
	if err != nil {
		t.Fatal(err)
	}
	if child.Realm == parent.Realm || child.Realm.ID == parent.Realm.ID {
		t.Fatal("iframe reused its parent Realm")
	}
	if child.DocumentGeneration() == parent.DocumentGeneration() {
		t.Fatal("iframe reused its parent document identity")
	}
	if owner, ok := child.FrameOwner(); !ok || owner != iframeHandle {
		t.Fatalf("child frame owner = %#v, %t", owner, ok)
	}
	content, ok := parent.ContentDocument(iframeHandle)
	if !ok || content.Document != child.DocumentGeneration() || content.Node != child.Document().RootID() {
		t.Fatalf("content document = %#v, %t", content, ok)
	}
	if err := parent.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := child.Realm.EnqueueTask(func(*browserruntime.TaskContext) error { return nil }); !errors.Is(err, browserruntime.ErrRealmClosed) {
		t.Fatalf("child Realm after parent close = %v, want ErrRealmClosed", err)
	}
}

func TestParentDocumentReplacementClosesChildRealm(t *testing.T) {
	t.Parallel()

	browser, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	location, _ := url.Parse("https://parent.test/")
	root := dom.NewDocument()
	iframe := dom.NewElement("iframe")
	root.AppendChild(iframe)
	parent, err := browser.NewPage(root, location)
	if err != nil {
		t.Fatal(err)
	}
	iframeID, _ := parent.Document().ID(iframe)
	child, err := parent.AttachChildFrame(
		NodeHandle{Document: parent.DocumentGeneration(), Node: iframeID},
		dom.NewDocument(),
		location,
	)
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := dom.IndexDocument(dom.NewDocument())
	if err != nil {
		t.Fatal(err)
	}
	parent.mutex.Lock()
	parent.nextNavigation++
	navigationID := parent.nextNavigation
	parent.navigation = navigationRecord{
		id:      navigationID,
		state:   NavigationLoadingDocument,
		context: context.Background(),
	}
	parent.mutex.Unlock()
	if _, err := parent.Realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		return parent.commitNavigationDocument(task, navigationID, preparedNavigation{
			document: replacement,
			location: location,
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := parent.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := child.Realm.EnqueueTask(func(*browserruntime.TaskContext) error { return nil }); !errors.Is(err, browserruntime.ErrRealmClosed) {
		t.Fatalf("child Realm after document replacement = %v, want ErrRealmClosed", err)
	}
}

func TestCrossDocumentImportAndAdoptCrossRealmQueues(t *testing.T) {
	t.Parallel()

	browser, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	location, _ := url.Parse("https://example.test/")
	sourceRoot := dom.NewDocument()
	parent := dom.NewElement("main")
	importedSource := dom.NewElement("section", dom.Attribute{Name: "id", Value: "copy"})
	importedSource.AppendChild(dom.NewText("copied"))
	adoptedSource := dom.NewElement("article", dom.Attribute{Name: "id", Value: "move"})
	adoptedSource.AppendChild(dom.NewText("moved"))
	parent.AppendChild(importedSource)
	parent.AppendChild(adoptedSource)
	sourceRoot.AppendChild(parent)
	source, err := browser.NewPage(sourceRoot, location)
	if err != nil {
		t.Fatal(err)
	}
	target, err := browser.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if source.DocumentGeneration() == target.DocumentGeneration() {
		t.Fatal("two Pages share a document generation")
	}
	copyID, _ := source.Document().ID(importedSource)
	copyHandle := NodeHandle{Document: source.DocumentGeneration(), Node: copyID}
	copyResult, err := target.QueueImportNodeFrom(source, copyHandle, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if target.Realm.Tasks.Len() != 1 {
		t.Fatalf("target queue after import producer = %d, want 1", target.Realm.Tasks.Len())
	}
	if err := target.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	copied := <-copyResult
	if copied.Err != nil || copied.Handle.Document != target.DocumentGeneration() {
		t.Fatalf("import result = %#v", copied)
	}
	if _, ok := source.Document().Resolve(copyID); !ok {
		t.Fatal("import retired its source identity")
	}
	copyNode, ok := target.Document().Resolve(copied.Handle.Node)
	if !ok || copyNode == importedSource || len(copyNode.Children) != 1 || copyNode.Children[0].Data != "copied" {
		t.Fatalf("imported node = %#v, found=%t", copyNode, ok)
	}
	if _, err := target.NodeFacadeRef(copied.Handle); err != nil {
		t.Fatalf("imported facade = %v", err)
	}

	moveID, _ := source.Document().ID(adoptedSource)
	moveHandle := NodeHandle{Document: source.DocumentGeneration(), Node: moveID}
	moveResult, err := target.QueueAdoptNodeFrom(source, moveHandle)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := source.Document().Resolve(moveID); ok {
		t.Fatal("adopted source identity still resolves")
	}
	if err := target.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	moved := <-moveResult
	if moved.Err != nil || moved.Handle.Document != target.DocumentGeneration() {
		t.Fatalf("adopt result = %#v", moved)
	}
	movedNode, ok := target.Document().Resolve(moved.Handle.Node)
	if !ok || movedNode == adoptedSource || len(movedNode.Children) != 1 || movedNode.Children[0].Data != "moved" {
		t.Fatalf("adopted node = %#v, found=%t", movedNode, ok)
	}
	if _, err := target.NodeFacadeRef(moved.Handle); err != nil {
		t.Fatalf("adopted facade = %v", err)
	}
	if err := browser.scheduler.Store().CheckInvariants(); err != nil {
		t.Fatal(err)
	}
}

func TestPagePostMessageStructuredClonesAndTransfersBuffer(t *testing.T) {
	t.Parallel()

	browser, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	location, _ := url.Parse("https://example.test/")
	source, err := browser.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	target, err := browser.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	var sourceRef memory.Ref
	var receivedRef memory.Ref
	if _, err := source.Realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		var allocErr error
		sourceRef, allocErr = task.NewArrayBuffer([]byte("message"))
		if allocErr != nil {
			return allocErr
		}
		_, postErr := source.PostMessage(task, target, func(next *browserruntime.TaskContext) error {
			if len(next.Refs) != 1 || next.Refs[0] == sourceRef {
				t.Fatalf("message refs = %#v, source = %s", next.Refs, sourceRef)
			}
			receivedRef = next.Refs[0]
			bytes, readErr := next.ReadArrayBuffer(receivedRef, 0, 7)
			if readErr != nil {
				return readErr
			}
			if string(bytes) != "message" {
				t.Fatalf("message bytes = %q", bytes)
			}
			return nil
		}, []memory.Ref{sourceRef}, sourceRef)
		if postErr != nil {
			return postErr
		}
		if _, readErr := task.ReadArrayBuffer(sourceRef, 0, 0); !errors.Is(readErr, memory.ErrDetachedBuffer) {
			t.Fatalf("postMessage source read = %v, want ErrDetachedBuffer", readErr)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := source.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if target.Realm.Tasks.Len() != 1 {
		t.Fatalf("target message queue = %d, want 1", target.Realm.Tasks.Len())
	}
	if err := target.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if receivedRef == (memory.Ref{}) {
		t.Fatal("message handler did not run")
	}
	if _, err := browser.scheduler.Store().DerefArrayBuffer(target.Realm.Owner(), receivedRef); !errors.Is(err, memory.ErrStaleRef) {
		t.Fatalf("message buffer after task = %v, want ErrStaleRef", err)
	}
}
