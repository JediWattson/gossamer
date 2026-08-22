package dom

// MutationRecordType identifies the native mutation journal entry kind.
type MutationRecordType uint8

const maxMutationJournalRecords = 4096

const (
	MutationChildList MutationRecordType = iota + 1
	MutationAttributes
	MutationCharacterData
	// MutationState is an internal native-control-state change. Script-facing
	// MutationObserver delivery ignores it, while incremental browser caches use
	// it to prove that every Document version change is journaled.
	MutationState
)

// MutationRecord is an immutable stable-ID journal entry. It contains enough
// native state for each Realm to apply its own MutationObserver filters.
type MutationRecord struct {
	Sequence        uint64
	Type            MutationRecordType
	Target          NodeID
	AddedNodes      []NodeID
	RemovedNodes    []NodeID
	PreviousSibling NodeID
	NextSibling     NodeID
	AttributeName   string
	OldValue        string
	OldValuePresent bool
	StateName       string
	Connected       bool
}

// MutationSequence returns the latest committed mutation journal sequence.
func (document *Document) MutationSequence() uint64 {
	if document == nil || document.store == nil {
		return 0
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	return document.mutationSequence
}

// TreeMutationSequence returns the sequence of the latest child-list change.
// Attribute, character-data, and native control-state mutations intentionally
// leave it unchanged so ownership mirrors can skip rebuilding stable-ID edges.
func (document *Document) TreeMutationSequence() uint64 {
	if document == nil || document.store == nil {
		return 0
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	return document.treeSequence
}

// MutationRecordsSince returns copies of records newer than sequence.
func (document *Document) MutationRecordsSince(sequence uint64) ([]MutationRecord, uint64, error) {
	if document == nil || document.store == nil {
		return nil, sequence, ErrInvalidDocument
	}
	document.store.mutex.RLock()
	defer document.store.mutex.RUnlock()
	return document.mutationRecordsSinceLocked(sequence)
}

func (document *Document) mutationRecordsSinceLocked(sequence uint64) ([]MutationRecord, uint64, error) {
	if len(document.mutations) != 0 {
		oldest := document.mutations[0].Sequence
		if oldest > 1 && sequence < oldest-1 {
			return nil, document.mutationSequence, ErrMutationJournalOverflow
		}
	}
	result := make([]MutationRecord, 0)
	for _, record := range document.mutations {
		if record.Sequence <= sequence {
			continue
		}
		copy := record
		copy.AddedNodes = append([]NodeID(nil), record.AddedNodes...)
		copy.RemovedNodes = append([]NodeID(nil), record.RemovedNodes...)
		result = append(result, copy)
	}
	return result, document.mutationSequence, nil
}

func (document *Document) appendMutationLocked(record MutationRecord) {
	document.mutationSequence++
	record.Sequence = document.mutationSequence
	if record.Type == MutationChildList {
		document.treeSequence = record.Sequence
	}
	if len(document.mutations) == maxMutationJournalRecords {
		copy(document.mutations, document.mutations[1:])
		document.mutations[len(document.mutations)-1] = record
		return
	}
	document.mutations = append(document.mutations, record)
}

func (document *Document) recordCharacterMutationLocked(node *Node, oldValue string) {
	target, found := document.store.ids[node]
	if !found {
		return
	}
	document.appendMutationLocked(MutationRecord{
		Type:            MutationCharacterData,
		Target:          target,
		OldValue:        oldValue,
		OldValuePresent: true,
		Connected:       document.nodeConnectedLocked(node),
	})
}

func (document *Document) recordAttributeMutationLocked(node *Node, name, oldValue string, oldValuePresent bool) {
	target, found := document.store.ids[node]
	if !found {
		return
	}
	document.appendMutationLocked(MutationRecord{
		Type:            MutationAttributes,
		Target:          target,
		AttributeName:   name,
		OldValue:        oldValue,
		OldValuePresent: oldValuePresent,
		Connected:       document.nodeConnectedLocked(node),
	})
}

func (document *Document) recordStateMutationLocked(node *Node, stateName string) {
	target, found := document.store.ids[node]
	if !found {
		return
	}
	document.appendMutationLocked(MutationRecord{
		Type:      MutationState,
		Target:    target,
		StateName: stateName,
		Connected: document.nodeConnectedLocked(node),
	})
}

func (document *Document) recordChildMutationLocked(parent *Node, before, after, moved []*Node) {
	if sameNodeSlice(before, after) {
		return
	}
	target, found := document.store.ids[parent]
	if !found {
		return
	}
	beforeSet := make(map[*Node]struct{}, len(before))
	afterSet := make(map[*Node]struct{}, len(after))
	for _, node := range before {
		beforeSet[node] = struct{}{}
	}
	for _, node := range after {
		afterSet[node] = struct{}{}
	}
	added := make([]*Node, 0)
	removed := make([]*Node, 0)
	for _, node := range after {
		if _, existed := beforeSet[node]; !existed {
			added = append(added, node)
		}
	}
	for _, node := range before {
		if _, remains := afterSet[node]; !remains {
			removed = append(removed, node)
		}
	}
	if len(added) == 0 && len(removed) == 0 {
		for _, node := range moved {
			if _, beforeFound := beforeSet[node]; !beforeFound {
				continue
			}
			if _, afterFound := afterSet[node]; afterFound {
				added = append(added, node)
				removed = append(removed, node)
			}
		}
	}
	previous, next := mutationSiblingContext(before, after, added, removed)
	document.appendMutationLocked(MutationRecord{
		Type:            MutationChildList,
		Target:          target,
		AddedNodes:      document.nodeIDsLocked(added),
		RemovedNodes:    document.nodeIDsLocked(removed),
		PreviousSibling: document.nodeIDLocked(previous),
		NextSibling:     document.nodeIDLocked(next),
		Connected:       document.nodeConnectedLocked(parent),
	})
}

func (document *Document) nodeConnectedLocked(node *Node) bool {
	if node == nil {
		return false
	}
	root, found := document.store.resolveLocked(document.root)
	if !found {
		return false
	}
	for node.Parent != nil {
		node = node.Parent
	}
	return node == root
}

func mutationSiblingContext(before, after, added, removed []*Node) (*Node, *Node) {
	if len(added) != 0 {
		first := added[0]
		last := added[len(added)-1]
		firstIndex := nodeIndex(after, first)
		lastIndex := nodeIndex(after, last)
		var previous *Node
		var next *Node
		if firstIndex > 0 {
			previous = after[firstIndex-1]
		}
		if lastIndex >= 0 && lastIndex+1 < len(after) {
			next = after[lastIndex+1]
		}
		return previous, next
	}
	if len(removed) != 0 {
		index := nodeIndex(before, removed[0])
		removedSet := make(map[*Node]struct{}, len(removed))
		for _, node := range removed {
			removedSet[node] = struct{}{}
		}
		var previous *Node
		for cursor := index - 1; cursor >= 0; cursor-- {
			if _, wasRemoved := removedSet[before[cursor]]; !wasRemoved {
				previous = before[cursor]
				break
			}
		}
		var next *Node
		for cursor := index + 1; cursor < len(before); cursor++ {
			if _, wasRemoved := removedSet[before[cursor]]; !wasRemoved {
				next = before[cursor]
				break
			}
		}
		return previous, next
	}
	return nil, nil
}

func (document *Document) nodeIDsLocked(nodes []*Node) []NodeID {
	result := make([]NodeID, 0, len(nodes))
	for _, node := range nodes {
		if id, found := document.store.ids[node]; found {
			result = append(result, id)
		}
	}
	return result
}

func (document *Document) nodeIDLocked(node *Node) NodeID {
	if node == nil {
		return InvalidNodeID
	}
	return document.store.ids[node]
}

func nodeIndex(nodes []*Node, target *Node) int {
	for index, node := range nodes {
		if node == target {
			return index
		}
	}
	return -1
}
