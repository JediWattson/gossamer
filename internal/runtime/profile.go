package runtime

import (
	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

// RealmProfile joins the physical RegionStore counters, semantic ownership
// counters, and runnable queue depths at one ordered-executor checkpoint. It
// deliberately contains no wall-clock timestamp: callers provide their own
// sequence labels so repeated workloads remain directly comparable.
type RealmProfile struct {
	Memory            memory.Stats            `json:"memory"`
	Ownership         ownership.Stats         `json:"ownership"`
	OwnershipPhysical ownership.PhysicalStats `json:"ownershipPhysical"`
	TaskDepth         int                     `json:"taskDepth"`
	MicrotaskDepth    int                     `json:"microtaskDepth"`
}

// Profile returns a point-in-time snapshot of the Go side of a Realm. Store,
// ledger, and queue snapshots are individually concurrency safe. A Realm's
// single logical executor should take profiles between tasks when an exact
// ownership boundary is required.
func (realm *Realm) Profile() RealmProfile {
	if realm == nil {
		return RealmProfile{}
	}
	return RealmProfile{
		Memory:            realm.store.Stats(),
		Ownership:         realm.ledger.Stats(),
		OwnershipPhysical: realm.ledger.PhysicalStats(),
		TaskDepth:         realm.Tasks.Len(),
		MicrotaskDepth:    realm.Microtasks.Len(),
	}
}
