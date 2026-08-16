package browser

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/JediWattson/gossamer/internal/dom"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

var (
	nextWrapperOwner  atomic.Uint64
	nextDocumentOwner atomic.Uint64
)

const browserNodeHostClass memory.HostClass = 1

// nodeLifetimeState mirrors one Document's stable-ID tree in the semantic
// ownership ledger. The document and the V8-facing wrapper/listener set are
// independent roots, so detachment, wrapper collection, or listener removal
// can release one claim without disturbing the other.
type nodeLifetimeState struct {
	generation DocumentGeneration
	document   *dom.Document
	ledger     *ownership.Ledger
	store      *memory.Store

	wrapperOwner  ownership.OwnerID
	wrapperRegion ownership.RegionID
	wrapperRoot   ownership.ObjectID
	wrappers      map[dom.NodeID]struct{}
	wrapperRoots  map[dom.NodeID]struct{}
	listeners     map[dom.NodeID]uint64

	documentOwner  ownership.OwnerID
	documentRegion ownership.RegionID
	documentRoot   ownership.ObjectID
	facadeRegion   memory.RegionID
	facades        map[dom.NodeID]memory.Ref

	nodes   map[dom.NodeID]ownership.ObjectID
	reverse map[ownership.ObjectID]dom.NodeID
	parents map[dom.NodeID]dom.NodeID
}

func newNodeLifetimeState(
	realm *browserruntime.Realm,
	document *dom.Document,
	generation DocumentGeneration,
) (_ *nodeLifetimeState, result error) {
	if realm == nil || realm.Ledger() == nil || realm.Store() == nil || document == nil || generation == 0 {
		return nil, fmt.Errorf("browser: invalid document lifetime boundary")
	}
	ledger := realm.Ledger()
	state := &nodeLifetimeState{
		generation:    generation,
		document:      document,
		ledger:        ledger,
		store:         realm.Store(),
		wrapperOwner:  ownership.OwnerID{Kind: ownership.OwnerWrapper, Value: nextWrapperOwner.Add(1)},
		documentOwner: ownership.OwnerID{Kind: ownership.OwnerDocument, Value: nextDocumentOwner.Add(1)},
		wrappers:      make(map[dom.NodeID]struct{}),
		wrapperRoots:  make(map[dom.NodeID]struct{}),
		listeners:     make(map[dom.NodeID]uint64),
		facades:       make(map[dom.NodeID]memory.Ref),
		nodes:         make(map[dom.NodeID]ownership.ObjectID),
		reverse:       make(map[ownership.ObjectID]dom.NodeID),
		parents:       make(map[dom.NodeID]dom.NodeID),
	}
	defer func() {
		if result != nil {
			result = errors.Join(result, state.close())
		}
	}()

	var err error
	state.wrapperRegion, err = ledger.CreateRegion(state.wrapperOwner)
	if err != nil {
		return nil, err
	}
	state.facadeRegion, err = state.store.NewRegion(state.documentOwner)
	if err != nil {
		return nil, err
	}
	state.documentRegion, err = ledger.OwnerRegion(state.documentOwner)
	if err != nil {
		return nil, err
	}
	state.wrapperRoot, err = ledger.CreateObject(state.wrapperRegion)
	if err != nil {
		return nil, err
	}
	state.documentRoot, err = ledger.CreateObject(state.documentRegion)
	if err != nil {
		return nil, err
	}
	if err := state.sync(nil); err != nil {
		return nil, err
	}
	return state, nil
}

// sync mirrors new nodes and changed parent edges, then reconciles both
// semantic root sets. Nodes first observed during a Page task are allocated in
// that task's short-lived region; parsed nodes start in the document region.
func (state *nodeLifetimeState) sync(task *browserruntime.TaskContext) error {
	if state == nil || state.document == nil {
		return fmt.Errorf("browser: document lifetime boundary is closed")
	}
	records := state.document.IdentitySnapshots()
	currentParents := make(map[dom.NodeID]dom.NodeID, len(records))
	for _, record := range records {
		currentParents[record.ID] = record.Parent
		if _, exists := state.nodes[record.ID]; exists {
			continue
		}
		facade, err := state.store.AllocHostObject(state.documentOwner, state.facadeRegion, memory.HostObject{
			Class:    browserNodeHostClass,
			Scope:    uint64(state.generation),
			Identity: uint64(record.ID),
		})
		if err != nil {
			return err
		}
		var (
			object    ownership.ObjectID
			objectErr error
		)
		if task != nil {
			object, objectErr = task.NewObject()
		} else {
			object, objectErr = state.ledger.CreateObject(state.documentRegion)
		}
		if objectErr != nil {
			_ = state.store.Free(state.documentOwner, facade)
			return objectErr
		}
		state.nodes[record.ID] = object
		state.reverse[object] = record.ID
		state.facades[record.ID] = facade
	}

	for node, oldParent := range state.parents {
		newParent, retained := currentParents[node]
		if !retained || oldParent == dom.InvalidNodeID || oldParent == newParent {
			continue
		}
		if err := state.ledger.RemoveReference(state.nodes[oldParent], state.nodes[node]); err != nil {
			return err
		}
		if err := state.ledger.RemoveReference(state.nodes[node], state.nodes[oldParent]); err != nil {
			return err
		}
	}
	for _, record := range records {
		if record.Parent == dom.InvalidNodeID || state.parents[record.ID] == record.Parent {
			continue
		}
		if err := state.ledger.AddReference(state.nodes[record.Parent], state.nodes[record.ID]); err != nil {
			return err
		}
		if err := state.ledger.AddReference(state.nodes[record.ID], state.nodes[record.Parent]); err != nil {
			return err
		}
	}
	state.parents = currentParents
	if err := state.reconcileWrapperRoots(); err != nil {
		return err
	}

	root := state.nodes[state.document.RootID()]
	if root == 0 {
		return fmt.Errorf("browser: document root has no lifetime object")
	}
	if err := state.ledger.AddReference(state.documentRoot, root); err != nil {
		return err
	}
	wrapperDestroyed, err := state.ledger.ReconcileRegion(state.wrapperOwner, []ownership.ObjectID{state.wrapperRoot})
	if err != nil {
		return err
	}
	documentRoots := make([]ownership.ObjectID, 0, len(state.facades)+1)
	documentRoots = append(documentRoots, state.documentRoot)
	for _, record := range records {
		facade := state.facades[record.ID]
		if facade == (memory.Ref{}) {
			return fmt.Errorf("browser: node %d has no facade record", record.ID)
		}
		object, objectErr := state.store.ObjectID(state.documentOwner, facade)
		if objectErr != nil {
			return objectErr
		}
		documentRoots = append(documentRoots, object)
	}
	documentDestroyed, err := state.ledger.ReconcileRegion(state.documentOwner, documentRoots)
	if err != nil {
		return err
	}
	return state.reclaim(append(wrapperDestroyed, documentDestroyed...))
}

func (state *nodeLifetimeState) retainWrapper(handle NodeHandle) error {
	if state == nil || handle.Document != state.generation || handle.Node == dom.InvalidNodeID {
		return ErrStaleNodeHandle
	}
	object := state.nodes[handle.Node]
	if object == 0 {
		return dom.ErrUnknownNode
	}
	if _, retained := state.wrappers[handle.Node]; retained {
		return nil
	}
	state.wrappers[handle.Node] = struct{}{}
	if !state.connected(handle.Node) {
		if err := state.ledger.AddReference(state.wrapperRoot, object); err != nil {
			delete(state.wrappers, handle.Node)
			return err
		}
		state.wrapperRoots[handle.Node] = struct{}{}
	}
	_, err := state.ledger.ReconcileRegion(state.wrapperOwner, []ownership.ObjectID{state.wrapperRoot})
	return err
}

func (state *nodeLifetimeState) releaseWrappers(handles []NodeHandle) error {
	if state == nil || len(handles) == 0 {
		return nil
	}
	for _, handle := range handles {
		if handle.Document != state.generation {
			continue
		}
		if _, retained := state.wrappers[handle.Node]; !retained {
			continue
		}
		object := state.nodes[handle.Node]
		if object == 0 {
			delete(state.wrappers, handle.Node)
			continue
		}
		delete(state.wrappers, handle.Node)
	}
	if err := state.reconcileWrapperRoots(); err != nil {
		return err
	}
	destroyed, err := state.ledger.ReconcileRegion(state.wrapperOwner, []ownership.ObjectID{state.wrapperRoot})
	if err != nil {
		return err
	}
	return state.reclaim(destroyed)
}

func (state *nodeLifetimeState) retainEventTarget(handle NodeHandle) error {
	if state == nil || handle.Document != state.generation || handle.Node == dom.InvalidNodeID {
		return ErrStaleNodeHandle
	}
	if state.nodes[handle.Node] == 0 {
		return dom.ErrUnknownNode
	}
	state.listeners[handle.Node]++
	if err := state.reconcileWrapperRoots(); err != nil {
		state.listeners[handle.Node]--
		if state.listeners[handle.Node] == 0 {
			delete(state.listeners, handle.Node)
		}
		return err
	}
	_, err := state.ledger.ReconcileRegion(state.wrapperOwner, []ownership.ObjectID{state.wrapperRoot})
	return err
}

func (state *nodeLifetimeState) releaseEventTarget(handle NodeHandle) error {
	if state == nil || handle.Document != state.generation || handle.Node == dom.InvalidNodeID {
		return ErrStaleNodeHandle
	}
	count := state.listeners[handle.Node]
	if count == 0 {
		return nil
	}
	if count == 1 {
		delete(state.listeners, handle.Node)
	} else {
		state.listeners[handle.Node] = count - 1
	}
	if err := state.reconcileWrapperRoots(); err != nil {
		state.listeners[handle.Node] = count
		return err
	}
	destroyed, err := state.ledger.ReconcileRegion(state.wrapperOwner, []ownership.ObjectID{state.wrapperRoot})
	if err != nil {
		return err
	}
	return state.reclaim(destroyed)
}

func (state *nodeLifetimeState) reconcileWrapperRoots() error {
	candidates := make(map[dom.NodeID]struct{}, len(state.wrappers)+len(state.listeners)+len(state.wrapperRoots))
	for node := range state.wrappers {
		candidates[node] = struct{}{}
	}
	for node := range state.listeners {
		candidates[node] = struct{}{}
	}
	for node := range state.wrapperRoots {
		candidates[node] = struct{}{}
	}
	for node := range candidates {
		_, rooted := state.wrapperRoots[node]
		_, wrapped := state.wrappers[node]
		needsRoot := !state.connected(node) && (wrapped || state.listeners[node] != 0)
		if needsRoot == rooted {
			continue
		}
		object := state.nodes[node]
		if object == 0 {
			return dom.ErrUnknownNode
		}
		if needsRoot {
			if err := state.ledger.AddReference(state.wrapperRoot, object); err != nil {
				return err
			}
			state.wrapperRoots[node] = struct{}{}
			continue
		}
		if err := state.ledger.RemoveReference(state.wrapperRoot, object); err != nil {
			return err
		}
		delete(state.wrapperRoots, node)
	}
	return nil
}

func (state *nodeLifetimeState) connected(node dom.NodeID) bool {
	root := state.document.RootID()
	seen := make(map[dom.NodeID]struct{})
	for node != dom.InvalidNodeID {
		if node == root {
			return true
		}
		if _, exists := seen[node]; exists {
			return false
		}
		seen[node] = struct{}{}
		node = state.parents[node]
	}
	return false
}

func (state *nodeLifetimeState) facade(handle NodeHandle) (memory.Ref, error) {
	if state == nil || handle.Document != state.generation || handle.Node == dom.InvalidNodeID {
		return memory.Ref{}, ErrStaleNodeHandle
	}
	ref := state.facades[handle.Node]
	if ref == (memory.Ref{}) {
		return memory.Ref{}, dom.ErrUnknownNode
	}
	record, err := state.store.DerefHostObject(state.documentOwner, ref)
	if err != nil {
		return memory.Ref{}, err
	}
	if record.Class != browserNodeHostClass || record.Scope != uint64(handle.Document) || record.Identity != uint64(handle.Node) {
		return memory.Ref{}, fmt.Errorf("browser: corrupt node facade record for %#v", handle)
	}
	return ref, nil
}

func (state *nodeLifetimeState) reclaim(objects []ownership.ObjectID) error {
	if len(objects) == 0 {
		return nil
	}
	seen := make(map[dom.NodeID]struct{}, len(objects))
	nodes := make([]dom.NodeID, 0, len(objects))
	for _, object := range objects {
		node := state.reverse[object]
		if node == dom.InvalidNodeID {
			continue
		}
		if _, exists := seen[node]; exists {
			continue
		}
		seen[node] = struct{}{}
		nodes = append(nodes, node)
	}
	if err := state.document.Reclaim(nodes); err != nil {
		return err
	}
	for _, node := range nodes {
		if facade := state.facades[node]; facade != (memory.Ref{}) {
			if err := state.store.Free(state.documentOwner, facade); err != nil {
				return err
			}
			delete(state.facades, node)
		}
		object := state.nodes[node]
		delete(state.reverse, object)
		delete(state.nodes, node)
		delete(state.parents, node)
		delete(state.wrappers, node)
		delete(state.wrapperRoots, node)
		delete(state.listeners, node)
	}
	return nil
}

func (state *nodeLifetimeState) close() error {
	if state == nil {
		return nil
	}
	var result error
	if state.wrapperRegion != 0 {
		result = errors.Join(result, state.ledger.CloseRegion(state.wrapperRegion))
		state.wrapperRegion = 0
	}
	if state.facadeRegion != 0 {
		result = errors.Join(result, state.store.ReleaseOwner(state.documentOwner))
		state.facadeRegion = 0
		state.documentRegion = 0
	}
	state.document = nil
	state.facades = nil
	return result
}
