package memory

import (
	"fmt"
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func BenchmarkCollectRegionIgnoresUnrelatedHeap(b *testing.B) {
	for _, unrelated := range []int{0, 10_000} {
		b.Run(fmt.Sprintf("unrelated-%d", unrelated), func(b *testing.B) {
			store := NewStore(nil)
			defer store.Close()
			owner := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: 9001}
			other := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: 9002}
			region, _ := store.NewRegion(owner)
			roots := make([]Ref, 128)
			for index := range roots {
				roots[index], _ = store.AllocCell(owner, region)
			}
			otherRegion, _ := store.NewRegion(other)
			for index := 0; index < unrelated; index++ {
				_, _ = store.AllocCell(other, otherRegion)
			}
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				if _, err := store.CollectRegion(owner, region, roots...); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRegionClosureChain(b *testing.B) {
	store := NewStore(nil)
	defer store.Close()
	owner := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: 9003}
	const regions = 2_048
	refs := make([]Ref, regions)
	for index := range refs {
		region, _ := store.NewRegion(owner)
		refs[index], _ = store.AllocCell(owner, region)
		if index != 0 {
			_ = store.Set(owner, refs[index-1], 0, RefValue(refs[index]))
		}
	}
	seed := map[RegionID]struct{}{refs[0].Region: {}}
	match := func(region *Region) bool { return region.State == RegionPrivate && region.Owner == owner }
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		store.mutex.Lock()
		closure := store.outgoingRegionsLocked(seed, match)
		store.mutex.Unlock()
		if len(closure) != regions {
			b.Fatalf("closure has %d regions, want %d", len(closure), regions)
		}
	}
}

func BenchmarkFreeWeakTargets(b *testing.B) {
	for _, entries := range []int{100, 1_000} {
		b.Run(fmt.Sprintf("entries-%d", entries), func(b *testing.B) {
			for iteration := 0; iteration < b.N; iteration++ {
				b.StopTimer()
				store := NewStore(nil)
				owner := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: uint64(iteration + 1)}
				region, _ := store.NewRegion(owner)
				table, _ := store.AllocWeakMap(owner, region)
				keys := make([]Ref, entries)
				for index := range keys {
					keys[index], _ = store.AllocCell(owner, region)
					_ = store.WeakMapSet(owner, table, keys[index], NumberValue(float64(index)))
				}
				b.StartTimer()
				for _, key := range keys {
					if err := store.Free(owner, key); err != nil {
						b.Fatal(err)
					}
				}
				b.StopTimer()
				_ = store.Close()
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*entries), "ns/target")
		})
	}
}

func BenchmarkReleaseOwnerIgnoresRetiredHistory(b *testing.B) {
	for _, retired := range []int{0, 10_000} {
		b.Run(fmt.Sprintf("retired-%d", retired), func(b *testing.B) {
			store := NewStore(nil)
			for index := 0; index < retired; index++ {
				owner := ownership.OwnerID{Kind: ownership.OwnerTask, Value: uint64(index + 1)}
				_, _ = store.NewRegion(owner)
				_ = store.ReleaseOwner(owner)
			}
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				owner := ownership.OwnerID{Kind: ownership.OwnerTask, Value: uint64(retired + iteration + 1)}
				_, _ = store.NewRegion(owner)
				if err := store.ReleaseOwner(owner); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			_ = store.Close()
		})
	}
}

func BenchmarkReleaseOwnerIgnoresUnrelatedRegionEdges(b *testing.B) {
	for _, unrelated := range []int{0, 10_000} {
		b.Run(fmt.Sprintf("edges-%d", unrelated), func(b *testing.B) {
			store := NewStore(nil)
			persistent := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: 91_000}
			var previous Ref
			for index := 0; index <= unrelated; index++ {
				region, _ := store.NewRegion(persistent)
				current, _ := store.AllocCell(persistent, region)
				if previous != (Ref{}) {
					_ = store.Set(persistent, previous, 0, RefValue(current))
				}
				previous = current
			}
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				owner := ownership.OwnerID{Kind: ownership.OwnerTask, Value: uint64(92_000 + iteration)}
				_, _ = store.NewRegion(owner)
				if err := store.ReleaseOwner(owner); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			_ = store.Close()
		})
	}
}

func BenchmarkEnsureOwnerIgnoresSemanticObjects(b *testing.B) {
	for _, objects := range []int{0, 10_000} {
		b.Run(fmt.Sprintf("objects-%d", objects), func(b *testing.B) {
			ledger := ownership.NewLedgerWithEventLimit(0)
			store := NewStore(ledger)
			owner := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: 92_500}
			if _, err := store.NewRegion(owner); err != nil {
				b.Fatal(err)
			}
			claim, _ := ledger.OwnerRegion(owner)
			for index := 0; index < objects; index++ {
				if _, err := ledger.CreateObject(claim); err != nil {
					b.Fatal(err)
				}
			}
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				store.mutex.Lock()
				_, err := store.ensureOwnerLocked(owner)
				store.mutex.Unlock()
				if err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			if err := store.Close(); err != nil {
				b.Fatal(err)
			}
		})
	}
}

func BenchmarkCollectEphemeronChain(b *testing.B) {
	for _, entries := range []int{1_000, 2_000} {
		b.Run(fmt.Sprintf("entries-%d", entries), func(b *testing.B) {
			store := NewStore(nil)
			owner := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: uint64(93_000 + entries)}
			region, _ := store.NewRegion(owner)
			table, _ := store.AllocWeakMap(owner, region)
			keys := make([]Ref, entries+1)
			for index := range keys {
				keys[index], _ = store.AllocCell(owner, region)
			}
			for index := 0; index < entries; index++ {
				_ = store.WeakMapSet(owner, table, keys[index], RefValue(keys[index+1]))
			}
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				result, err := store.Collect(owner, table, keys[0])
				if err != nil || result.MarkedSlots != uint64(entries+2) {
					b.Fatalf("Collect() = %#v, %v", result, err)
				}
			}
			b.StopTimer()
			_ = store.Close()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*entries), "ns/entry")
		})
	}
}

func BenchmarkCopyEphemeronChain(b *testing.B) {
	for _, entries := range []int{1_000, 2_000} {
		b.Run(fmt.Sprintf("entries-%d", entries), func(b *testing.B) {
			store := NewStore(nil)
			source := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: uint64(94_000 + entries)}
			region, _ := store.NewRegion(source)
			table, _ := store.AllocWeakMap(source, region)
			keys := make([]Ref, entries+1)
			for index := range keys {
				keys[index], _ = store.AllocCell(source, region)
			}
			for index := 0; index < entries; index++ {
				_ = store.WeakMapSet(source, table, keys[index], RefValue(keys[index+1]))
			}
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				destination := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: uint64(1_000_000 + entries*10_000 + iteration)}
				copied, err := store.Copy(source, destination, table, keys[0])
				if err != nil || len(copied) != 2 {
					b.Fatalf("Copy() = %v, %v", copied, err)
				}
				b.StopTimer()
				if err := store.ReleaseOwner(destination); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
			}
			b.StopTimer()
			_ = store.Close()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*entries), "ns/entry")
		})
	}
}
