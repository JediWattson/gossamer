package nativeengine

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

type canonicalCacheEntry struct {
	key   string
	value memory.Ref
}

type canonicalCache struct {
	ref     memory.Ref
	wrapper bool
	entries []canonicalCacheEntry
}

// CollectGarbage runs the native owner-local tracing collector between tasks.
// Canonical identity caches are evacuated before tracing so they do not turn
// weak wrapper identity into lifetime ownership. Surviving values are restored
// under newly allocated keys after the checkpoint.
func (realm *Realm) CollectGarbage(lifetimes ...browser.NodeWrapperLifetimeHost) error {
	if realm == nil {
		return ErrRealmClosed
	}
	realm.mutex.Lock()
	handles, err := realm.collectGarbageLocked(len(lifetimes) != 0)
	realm.mutex.Unlock()
	if err != nil {
		return err
	}
	if len(handles) == 0 {
		return nil
	}
	if len(lifetimes) == 0 || lifetimes[0] == nil {
		return fmt.Errorf("nativeengine: host cannot release %d collected DOM wrappers", len(handles))
	}
	return lifetimes[0].ReleaseNodeWrappers(handles)
}

func (realm *Realm) collectGarbageLocked(drainWrappers bool) ([]browser.NodeHandle, error) {
	if realm.closed {
		return nil, ErrRealmClosed
	}
	if realm.activeTask != 0 {
		return nil, ErrCheckpointRequired
	}
	if realm.runtime == nil || realm.persistent == nil {
		return nil, nil
	}

	store := realm.runtime.Store()
	owner := realm.runtime.Owner()
	caches := []canonicalCache{
		{ref: realm.persistentWrapperCache, wrapper: true},
		{ref: realm.persistentFacadeCache},
		{ref: realm.persistentCollectionCache},
	}
	for index := range caches {
		entries, err := snapshotCanonicalCache(store, owner, caches[index].ref)
		if err != nil {
			return nil, err
		}
		caches[index].entries = entries
	}
	for _, cache := range caches {
		if err := store.MapClear(owner, cache.ref); err != nil {
			return nil, err
		}
	}

	_, collectErr := realm.runtime.CollectNative(realm.persistent.Roots()...)
	collected, restoreErr := realm.restoreCanonicalCachesLocked(caches)
	if collectErr != nil || restoreErr != nil {
		return nil, errors.Join(collectErr, restoreErr)
	}
	realm.collectedWrappers = append(realm.collectedWrappers, collected...)
	if err := store.CheckInvariants(); err != nil {
		return nil, err
	}
	if !drainWrappers || len(realm.collectedWrappers) == 0 {
		return nil, nil
	}
	handles := append([]browser.NodeHandle(nil), realm.collectedWrappers...)
	realm.collectedWrappers = nil
	return handles, nil
}

func snapshotCanonicalCache(store *memory.Store, owner ownership.OwnerID, ref memory.Ref) ([]canonicalCacheEntry, error) {
	if ref == (memory.Ref{}) {
		return nil, fmt.Errorf("nativeengine: canonical cache is unavailable")
	}
	cache, err := store.DerefMap(owner, ref)
	if err != nil {
		return nil, err
	}
	entries := make([]canonicalCacheEntry, 0, len(cache.Entries))
	for _, entry := range cache.Entries {
		if !entry.Key.IsRef() || !entry.Value.IsRef() {
			return nil, fmt.Errorf("nativeengine: canonical cache entry is not Ref-backed")
		}
		key, err := store.DerefString(owner, entry.Key.Ref())
		if err != nil {
			return nil, err
		}
		entries = append(entries, canonicalCacheEntry{key: key, value: entry.Value.Ref()})
	}
	return entries, nil
}

func (realm *Realm) restoreCanonicalCachesLocked(caches []canonicalCache) ([]browser.NodeHandle, error) {
	store := realm.runtime.Store()
	owner := realm.runtime.Owner()
	collected := make([]browser.NodeHandle, 0)
	for _, cache := range caches {
		for _, entry := range cache.entries {
			if _, err := store.Kind(owner, entry.value); err != nil {
				if !errors.Is(err, memory.ErrStaleRef) {
					return nil, err
				}
				if cache.wrapper {
					handle, err := parseNodeCacheKey(entry.key)
					if err != nil {
						return nil, err
					}
					collected = append(collected, handle)
				}
				continue
			}
			key, err := store.AllocString(owner, realm.persistentRegion, entry.key)
			if err != nil {
				return nil, err
			}
			if err := store.MapSet(owner, cache.ref, memory.RefValue(key), memory.RefValue(entry.value)); err != nil {
				_ = store.Free(owner, key)
				return nil, err
			}
		}
	}
	return collected, nil
}

func parseNodeCacheKey(key string) (browser.NodeHandle, error) {
	documentText, nodeText, found := strings.Cut(key, ":")
	if !found {
		return browser.NodeHandle{}, fmt.Errorf("nativeengine: invalid node wrapper cache key %q", key)
	}
	document, err := strconv.ParseUint(documentText, 10, 64)
	if err != nil {
		return browser.NodeHandle{}, fmt.Errorf("nativeengine: invalid node wrapper document %q: %w", documentText, err)
	}
	node, err := strconv.ParseUint(nodeText, 10, 32)
	if err != nil {
		return browser.NodeHandle{}, fmt.Errorf("nativeengine: invalid node wrapper id %q: %w", nodeText, err)
	}
	return browser.NodeHandle{Document: browser.DocumentGeneration(document), Node: dom.NodeID(node)}, nil
}
