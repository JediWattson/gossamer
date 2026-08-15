package memory

import (
	"fmt"
	"math"
	"sort"
)

// RegionEdge is one counted directed edge in the region graph.
type RegionEdge struct {
	From  RegionID
	To    RegionID
	Count uint32
}

type edgeKey struct {
	from RegionID
	to   RegionID
}

// Barrier maintains counted region-to-region references. Store serializes all
// access to it, so Barrier itself intentionally has no second mutex.
type Barrier struct {
	edges map[edgeKey]uint32
}

func newBarrier() *Barrier {
	return &Barrier{edges: make(map[edgeKey]uint32)}
}

func (barrier *Barrier) link(from, to RegionID) error {
	if from == to {
		return nil
	}
	key := edgeKey{from: from, to: to}
	if barrier.edges[key] == math.MaxUint32 {
		return fmt.Errorf("memory: region edge R%d -> R%d count overflow", from, to)
	}
	barrier.edges[key]++
	return nil
}

func (barrier *Barrier) unlink(from, to RegionID) error {
	if from == to {
		return nil
	}
	key := edgeKey{from: from, to: to}
	count := barrier.edges[key]
	if count == 0 {
		return fmt.Errorf("memory: missing region edge R%d -> R%d", from, to)
	}
	if count == 1 {
		delete(barrier.edges, key)
		return nil
	}
	barrier.edges[key] = count - 1
	return nil
}

func (barrier *Barrier) count(from, to RegionID) uint32 {
	return barrier.edges[edgeKey{from: from, to: to}]
}

func (barrier *Barrier) snapshot() []RegionEdge {
	edges := make([]RegionEdge, 0, len(barrier.edges))
	for key, count := range barrier.edges {
		edges = append(edges, RegionEdge{From: key.from, To: key.to, Count: count})
	}
	sort.Slice(edges, func(left, right int) bool {
		if edges[left].From != edges[right].From {
			return edges[left].From < edges[right].From
		}
		return edges[left].To < edges[right].To
	})
	return edges
}
