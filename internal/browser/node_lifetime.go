package browser

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/JediWattson/gossamer/internal/dom"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

var (
	nextWrapperOwner  atomic.Uint64
	nextDocumentOwner atomic.Uint64
)

// nodeLifetimeState mirrors one Document's stable-ID tree in the semantic
// ownership ledger. The document and the V8-facing wrapper/listener set are
// independent roots, so detachment, wrapper collection, or listener removal
// can release one claim without disturbing the other.
type nodeLifetimeState struct {
	generation DocumentGeneration
	document   *dom.Document
	ledger     *ownership.Ledger

	wrapperOwner  ownership.OwnerID
	wrapperRegion ownership.RegionID
	wrapperRoot   ownership.ObjectID
	wrappers      map[dom.NodeID]struct{}
	wrapperRoots  map[dom.NodeID]struct{}
	listeners     map[dom.NodeID]uint64

	documentOwner  ownership.OwnerID
	documentRegion ownership.RegionID
	documentRoot   ownership.ObjectID

	nodes   map[dom.NodeID]ownership.ObjectID
	reverse map[ownership.ObjectID]dom.NodeID
	parents map[dom.NodeID]dom.NodeID
}

func newNodeLifetimeState(
	ledger *ownership.Ledger,
	document *dom.Document,
	generation DocumentGeneration,
) (_ *nodeLifetimeState, result error) {
	if ledger == nil || document == nil || generation == 0 {
		return nil, fmt.Errorf("browser: invalid document lifetime boundary")
	}
	state := &nodeLifetimeState{
		generation:    generation,
		document:      document,
		ledger:        ledger,
		wrapperOwner:  ownership.OwnerID{Kind: ownership.OwnerWrapper, Value: nextWrapperOwner.Add(1)},
		documentOwner: ownership.OwnerID{Kind: ownership.OwnerDocument, Value: nextDocumentOwner.Add(1)},
		wrappers:      make(map[dom.NodeID]struct{}),
		wrapperRoots:  make(map[dom.NodeID]struct{}),
		listeners:     make(map[dom.NodeID]uint64),
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
	state.documentRegion, err = ledger.CreateRegion(state.documentOwner)
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
		var (
			object ownership.ObjectID
			err    error
		)
		if task != nil {
			object, err = task.NewObject()
		} else {
			object, err = state.ledger.CreateObject(state.documentRegion)
		}
		if err != nil {
			return err
		}
		state.nodes[record.ID] = object
		state.reverse[object] = record.ID
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
	documentDestroyed, err := state.ledger.ReconcileRegion(state.documentOwner, []ownership.ObjectID{state.documentRoot})
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
	if state.documentRegion != 0 {
		result = errors.Join(result, state.ledger.CloseRegion(state.documentRegion))
		state.documentRegion = 0
	}
	state.document = nil
	return result
}
