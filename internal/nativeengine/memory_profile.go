package nativeengine

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/browser"
	browserruntime "github.com/JediWattson/gossamer/internal/runtime"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

// DefaultCheckpointCollectionInterval bounds persistent weak-cache growth
// while avoiding a full native trace after every small browser task.
const DefaultCheckpointCollectionInterval uint64 = 4096

// DefaultActiveCollectionInterval bounds temporary values created by one long
// browser callback before it can reach an ordinary task checkpoint.
const DefaultActiveCollectionInterval uint64 = 250_000

// Sampling the allocation counter at explicit interpreter safepoints keeps the
// policy deterministic without placing a Store lock on every allocation.
const activeCollectionCheckInstructions uint64 = 100_000

// observeExecutionLocked runs only between bytecode instructions while the
// Realm executor and native-engine mutex are already held.
func (realm *Realm) observeExecutionLocked(context *browserruntime.TaskContext, sample browserruntime.ExecutionSample) error {
	if realm == nil || context == nil || context.Realm == nil {
		return nil
	}
	if err := realm.collectActiveScratchLocked(context); err != nil {
		return err
	}
	if realm.executionSample == nil || realm.executionSampleInterval == 0 {
		return nil
	}
	if realm.lastExecutionSampleTask != sample.TaskID || sample.RawInstructions < realm.lastExecutionSampleRaw {
		realm.lastExecutionSampleTask = sample.TaskID
		realm.lastExecutionSampleRaw = 0
	}
	if sample.RawInstructions-realm.lastExecutionSampleRaw < realm.executionSampleInterval {
		return nil
	}
	realm.lastExecutionSampleRaw = sample.RawInstructions
	return realm.executionSample(context, sample)
}

func (realm *Realm) collectActiveScratchLocked(context *browserruntime.TaskContext) error {
	if realm.persistentRegion == 0 || realm.activeRegion == 0 || realm.activeRegion == realm.persistentRegion ||
		context.MemoryRegion != realm.activeRegion || realm.activeCollectionInterval == 0 {
		return nil
	}
	store := context.Realm.Store()
	allocations := store.Stats().Allocations
	if realm.lastActiveCollection == 0 {
		realm.lastActiveCollection = allocations
	}
	if allocations >= realm.lastActiveCollection && allocations-realm.lastActiveCollection < realm.activeCollectionInterval {
		return nil
	}
	roots := make([]memory.Ref, 0, context.StackDepth()*8+32)
	metadata := make(map[memory.RegionID]memory.RegionMetadata)
	if err := context.VisitRefs(func(ref memory.Ref) error {
		region, found := metadata[ref.Region]
		if !found {
			var err error
			region, err = store.RegionMetadata(ref.Region)
			if err != nil {
				return err
			}
			metadata[ref.Region] = region
		}
		if region.State != memory.RegionPrivate || region.Owner == context.Owner {
			roots = append(roots, ref)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("nativeengine: visit active scratch roots: %w", err)
	}
	collection, err := store.CollectRegion(context.Owner, realm.activeRegion, roots...)
	if err != nil {
		return fmt.Errorf("nativeengine: collect active scratch region R%d: %w", realm.activeRegion, err)
	}
	realm.activeCollections++
	realm.activeCollectedSlots += collection.ReclaimedSlots
	realm.lastActiveCollection = store.Stats().Allocations
	return nil
}

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
		ResizeObservers:       uint64(len(realm.resizeObservers)),
		Modules:               uint64(len(realm.modules)),
		CheckpointCollections: realm.checkpointCollections,
		ActiveCollections:     realm.activeCollections,
		ActiveCollectedSlots:  realm.activeCollectedSlots,
		ReportedExceptions:    realm.reportedExceptions,
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
