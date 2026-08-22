package runtime

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
)

// WithMemoryRegion returns an execution view over another private region
// already owned by the current task. It is used after a Realm graph has been
// explicitly copied into the task; the returned context adds no ownership.
func (context *TaskContext) WithMemoryRegion(region memory.RegionID, intrinsics *Intrinsics) (*TaskContext, error) {
	if context == nil || context.Realm == nil {
		return nil, fmt.Errorf("runtime: nil task context")
	}
	if region == 0 {
		return nil, fmt.Errorf("runtime: invalid memory region 0")
	}
	snapshot, err := context.Realm.store.RegionMetadata(region)
	if err != nil {
		return nil, err
	}
	if snapshot.State != memory.RegionPrivate || snapshot.Owner != context.Owner {
		return nil, fmt.Errorf("%w: region R%d is not private storage owned by %s", memory.ErrAccessDenied, region, context.Owner)
	}
	return &TaskContext{
		Realm:        context.Realm,
		TaskID:       context.TaskID,
		Owner:        context.Owner,
		Region:       context.Region,
		MemoryRegion: region,
		Refs:         append([]memory.Ref(nil), context.Refs...),
		intrinsics:   intrinsics,
		stack:        context.stack,
		jobs:         context.jobs,
	}, nil
}

// WithBorrowedRealmMemoryRegion returns an execution view over private Realm
// storage while preserving the current task's ordered-executor identity. The
// returned context borrows the Realm's ownership authority; it does not add a
// claim or transfer the region to the task.
func (context *TaskContext) WithBorrowedRealmMemoryRegion(region memory.RegionID, intrinsics *Intrinsics) (*TaskContext, error) {
	if context == nil || context.Realm == nil {
		return nil, fmt.Errorf("runtime: nil task context")
	}
	if region == 0 {
		return nil, fmt.Errorf("runtime: invalid memory region 0")
	}
	snapshot, err := context.Realm.store.RegionMetadata(region)
	if err != nil {
		return nil, err
	}
	if snapshot.State != memory.RegionPrivate || snapshot.Owner != context.Realm.owner {
		return nil, fmt.Errorf("%w: region R%d is not private storage owned by %s", memory.ErrAccessDenied, region, context.Realm.owner)
	}
	return &TaskContext{
		Realm:        context.Realm,
		TaskID:       context.TaskID,
		Owner:        context.Realm.owner,
		Region:       context.Realm.region,
		MemoryRegion: region,
		Refs:         append([]memory.Ref(nil), context.Refs...),
		intrinsics:   intrinsics,
		stack:        context.stack,
		jobs:         context.jobs,
	}, nil
}
