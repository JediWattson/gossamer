package browser

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/dom"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
)

func TestNodeFacadeRecordsAreCanonicalAndFollowNativeLifetime(t *testing.T) {
	browserRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	page, err := browserRuntime.NewPage(dom.NewDocument(), &url.URL{Scheme: "https", Host: "gossamer.test"})
	if err != nil {
		t.Fatal(err)
	}
	root := NodeHandle{Document: page.DocumentGeneration(), Node: page.Document().RootID()}
	first, err := page.NodeFacadeRef(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := page.NodeFacadeRef(root)
	if err != nil || second != first {
		t.Fatalf("canonical root facade = %s then %s, %v", first, second, err)
	}
	baseline := page.Realm.Profile().Memory
	if baseline.LiveHostObjects != uint64(page.Document().Store().LiveLen()) {
		t.Fatalf("initial facade records = %#v, live nodes=%d", baseline, page.Document().Store().LiveLen())
	}

	var detached NodeHandle
	if _, err := page.Realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		host := &taskHost{page: page, task: task, generation: page.DocumentGeneration()}
		detached, err = host.CreateElement("aside")
		if err != nil {
			return err
		}
		return host.RetainNodeWrapper(detached)
	}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	detachedRef, err := page.NodeFacadeRef(detached)
	if err != nil || detachedRef == first {
		t.Fatalf("detached facade = %s, %v", detachedRef, err)
	}
	if live := page.Realm.Profile().Memory.LiveHostObjects; live != baseline.LiveHostObjects+1 {
		t.Fatalf("live facade records = %d, want %d", live, baseline.LiveHostObjects+1)
	}

	if err := page.ReleaseNodeWrappers([]NodeHandle{detached}); err != nil {
		t.Fatal(err)
	}
	if _, err := page.NodeFacadeRef(detached); !errors.Is(err, dom.ErrUnknownNode) {
		t.Fatalf("reclaimed NodeFacadeRef() error = %v, want unknown node", err)
	}
	if stats := page.Realm.Profile().Memory; stats.LiveHostObjects != baseline.LiveHostObjects {
		t.Fatalf("reclaimed facade stats = %#v", stats)
	}
	if err := page.Realm.Store().CheckInvariants(); err != nil {
		t.Fatalf("region-backed facade invariants: %v", err)
	}
}

func TestDetachedWrapperGraphOutlivesTaskThenReclaims(t *testing.T) {
	browserRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	page, err := browserRuntime.NewPage(dom.NewDocument(), &url.URL{Scheme: "https", Host: "gossamer.test"})
	if err != nil {
		t.Fatal(err)
	}
	document := page.Document()
	baselineNodes := document.Store().LiveLen()
	baselineOwnership := browserRuntime.Ledger().Stats()
	var root NodeHandle
	var child NodeHandle

	if _, err := page.Realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		host := &taskHost{page: page, task: task, generation: page.DocumentGeneration()}
		root, err = host.CreateElement("div")
		if err != nil {
			return err
		}
		child, err = host.CreateTextNode("temporary")
		if err != nil {
			return err
		}
		if err := host.AppendChild(root, child); err != nil {
			return err
		}
		// A child wrapper retains the complete detached tree component through
		// the DOM's bidirectional parent/child ownership graph.
		return host.RetainNodeWrapper(child)
	}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	afterTask := browserRuntime.Ledger().Stats()
	if afterTask.TaskLocalAllocations-baselineOwnership.TaskLocalAllocations != 2 ||
		afterTask.LiveObjects-baselineOwnership.LiveObjects != 4 ||
		document.Store().LiveLen()-baselineNodes != 2 {
		t.Fatalf("construction region after task = nodes:%d ownership:%#v baseline:%#v",
			document.Store().LiveLen()-baselineNodes, afterTask, baselineOwnership)
	}
	if _, ok := document.Resolve(root.Node); !ok {
		t.Fatal("wrapper-retained root died with its construction task")
	}

	if _, err := page.Realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		host := &taskHost{page: page, task: task, generation: page.DocumentGeneration()}
		return host.ReleaseNodeWrappers([]NodeHandle{child})
	}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	afterCollection := browserRuntime.Ledger().Stats()
	if afterCollection.LiveObjects != baselineOwnership.LiveObjects ||
		afterCollection.ObjectsDestroyed-baselineOwnership.ObjectsDestroyed != 4 ||
		document.Store().LiveLen() != baselineNodes {
		t.Fatalf("wrapper collection = liveNodes:%d ownership:%#v baseline:%#v",
			document.Store().LiveLen(), afterCollection, baselineOwnership)
	}
	if _, ok := document.Resolve(root.Node); ok {
		t.Fatal("collected detached wrapper root still resolves")
	}
	if _, ok := document.Resolve(child.Node); ok {
		t.Fatal("collected wrapper's reachable child still resolves")
	}
}

func TestConnectedWrapperActivatesBeforeDocumentDetachment(t *testing.T) {
	rootNode := dom.NewDocument()
	html := dom.NewElement("html")
	body := dom.NewElement("body")
	row := dom.NewElement("div")
	row.AppendChild(dom.NewText("retained"))
	rootNode.AppendChild(html)
	html.AppendChild(body)
	body.AppendChild(row)

	browserRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	page, err := browserRuntime.NewPage(rootNode, &url.URL{Scheme: "https", Host: "gossamer.test"})
	if err != nil {
		t.Fatal(err)
	}
	document := page.Document()
	bodyID, _ := document.ID(body)
	rowID, _ := document.ID(row)
	bodyHandle := NodeHandle{Document: page.DocumentGeneration(), Node: bodyID}
	rowHandle := NodeHandle{Document: page.DocumentGeneration(), Node: rowID}
	baseline := browserRuntime.Ledger().Stats()
	baselineNodes := document.Store().LiveLen()
	if err := page.RetainNodeWrapper(rowHandle); err != nil {
		t.Fatal(err)
	}
	if afterRetain := browserRuntime.Ledger().Stats(); afterRetain != baseline {
		t.Fatalf("connected wrapper duplicated document ownership: before=%#v after=%#v", baseline, afterRetain)
	}

	if _, err := page.Realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		host := &taskHost{page: page, task: task, generation: page.DocumentGeneration()}
		return host.RemoveChild(bodyHandle, rowHandle)
	}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := document.Resolve(rowID); !ok || document.Store().LiveLen() != baselineNodes {
		t.Fatal("live wrapper did not retain subtree across document detachment")
	}
	afterDetach := browserRuntime.Ledger().Stats()
	if afterDetach.LiveObjects != baseline.LiveObjects ||
		afterDetach.RetainOperations <= baseline.RetainOperations ||
		afterDetach.ReleaseOperations <= baseline.ReleaseOperations {
		t.Fatalf("detachment ownership handoff: before=%#v after=%#v", baseline, afterDetach)
	}

	if err := page.ReleaseNodeWrappers([]NodeHandle{rowHandle}); err != nil {
		t.Fatal(err)
	}
	if _, ok := document.Resolve(rowID); ok || document.Store().LiveLen() != baselineNodes-2 {
		t.Fatal("detached subtree survived its final wrapper root")
	}
}

func TestDetachedEventTargetOutlivesWrapperUntilFinalListenerRelease(t *testing.T) {
	browserRuntime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	page, err := browserRuntime.NewPage(dom.NewDocument(), &url.URL{Scheme: "https", Host: "gossamer.test"})
	if err != nil {
		t.Fatal(err)
	}
	document := page.Document()
	baselineNodes := document.Store().LiveLen()
	baseline := browserRuntime.Ledger().Stats()
	var target NodeHandle

	if _, err := page.Realm.EnqueueTask(func(task *browserruntime.TaskContext) error {
		host := &taskHost{page: page, task: task, generation: page.DocumentGeneration()}
		target, err = host.CreateElement("button")
		if err != nil {
			return err
		}
		if err := host.RetainNodeWrapper(target); err != nil {
			return err
		}
		if err := host.RetainNodeEventTarget(target); err != nil {
			return err
		}
		return host.RetainNodeEventTarget(target)
	}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := page.ReleaseNodeWrappers([]NodeHandle{target}); err != nil {
		t.Fatal(err)
	}
	if _, ok := document.Resolve(target.Node); !ok {
		t.Fatal("listener-owned detached target died with its weak wrapper")
	}
	if err := page.ReleaseNodeEventTarget(target); err != nil {
		t.Fatal(err)
	}
	if _, ok := document.Resolve(target.Node); !ok {
		t.Fatal("first listener release removed a target with another listener")
	}
	if err := page.ReleaseNodeEventTarget(target); err != nil {
		t.Fatal(err)
	}
	if _, ok := document.Resolve(target.Node); ok || document.Store().LiveLen() != baselineNodes {
		t.Fatal("detached target survived its final listener claim")
	}
	after := browserRuntime.Ledger().Stats()
	if after.LiveObjects != baseline.LiveObjects ||
		after.ObjectsDestroyed-baseline.ObjectsDestroyed != 2 {
		t.Fatalf("listener lifetime ownership = before:%#v after:%#v", baseline, after)
	}
}
