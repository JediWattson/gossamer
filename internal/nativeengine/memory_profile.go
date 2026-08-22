package nativeengine

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

// DefaultCheckpointCollectionInterval bounds persistent weak-cache growth
// while avoiding a full native trace after every small browser task.
const DefaultCheckpointCollectionInterval uint64 = 4096

// CollectCheckpoint runs Strand's existing weak-cache-aware collector after a
// deterministic number of RegionStore allocations. The browser invokes this
// only after draining the Page task queue.
func (realm *Realm) CollectCheckpoint(lifetime browser.NodeWrapperLifetimeHost) (bool, error) {
	if realm == nil {
		return false, ErrRealmClosed
	}
	realm.mutex.Lock()
	if realm.closed {
		realm.mutex.Unlock()
		return false, ErrRealmClosed
	}
	if realm.activeTask != 0 {
		realm.mutex.Unlock()
		return false, ErrCheckpointRequired
	}
	runtimeRealm := realm.runtime
	persistent := realm.persistent
	interval := realm.checkpointCollectionInterval
	last := realm.lastCheckpointCollection
	realm.mutex.Unlock()
	if runtimeRealm == nil || persistent == nil {
		return false, nil
	}
	allocations := runtimeRealm.Store().Stats().Allocations
	if allocations >= last && allocations-last < interval {
		return false, nil
	}
	if err := realm.CollectGarbage(lifetime); err != nil {
		return true, err
	}
	allocations = runtimeRealm.Store().Stats().Allocations
	realm.mutex.Lock()
	realm.lastCheckpointCollection = allocations
	realm.checkpointCollections++
	realm.mutex.Unlock()
	return true, nil
}

// ScriptMemoryProfile attributes Realm-owned retention roots without cloning
// their backing Map entries.
func (realm *Realm) ScriptMemoryProfile() (browser.ScriptMemoryProfile, error) {
	if realm == nil {
		return browser.ScriptMemoryProfile{}, ErrRealmClosed
	}
	realm.mutex.Lock()
	defer realm.mutex.Unlock()
	if realm.closed {
		return browser.ScriptMemoryProfile{}, ErrRealmClosed
	}
	profile := browser.ScriptMemoryProfile{
		Engine:                "strand",
		Timers:                uint64(len(realm.timerCallbacks)),
		Intervals:             uint64(len(realm.intervalCallbacks)),
		AnimationFrames:       uint64(len(realm.animationCallbacks)),
		MutationObservers:     uint64(len(realm.mutationObservers)),
		Modules:               uint64(len(realm.modules)),
		CheckpointCollections: realm.checkpointCollections,
	}
	for _, listeners := range realm.listeners {
		profile.EventListeners += uint64(len(listeners))
	}
	if realm.runtime == nil || realm.persistent == nil {
		return profile, nil
	}
	store := realm.runtime.Store()
	owner := realm.runtime.Owner()
	stats := store.Stats()
	profile.LiveValues = stats.LiveSlots
	profile.LiveWrappers = stats.LiveHostObjects
	profile.Collections = stats.Collections
	cacheFields := []struct {
		ref   memory.Ref
		value *uint64
	}{
		{realm.persistentWrapperCache, &profile.WrapperCacheEntries},
		{realm.persistentFacadeCache, &profile.FacadeCacheEntries},
		{realm.persistentCollectionCache, &profile.CollectionCacheEntries},
		{realm.persistentCallbackCache, &profile.CallbackCacheEntries},
		{realm.persistentObserverCache, &profile.ObserverCacheEntries},
		{realm.persistentModuleCache, &profile.ModuleCacheEntries},
	}
	for _, field := range cacheFields {
		if field.ref == (memory.Ref{}) {
			continue
		}
		length, err := store.MapLen(owner, field.ref)
		if err != nil {
			return browser.ScriptMemoryProfile{}, fmt.Errorf("nativeengine: profile cache %s: %w", field.ref, err)
		}
		*field.value = uint64(length)
	}
	profile.LiveCallbacks = profile.CallbackCacheEntries
	return profile, nil
}

var _ browser.JSCheckpointCollector = (*Realm)(nil)
var _ browser.JSMemoryProfiler = (*Realm)(nil)
