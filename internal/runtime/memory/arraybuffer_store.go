package memory

import (
	"fmt"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func (store *Store) AllocArrayBuffer(owner ownership.OwnerID, regionID RegionID, bytes []byte) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	ref, err := store.allocKindLocked(owner, regionID, HeapArrayBuffer, false)
	if err != nil {
		return Ref{}, err
	}
	_, slot, _ := store.slotLocked(ref)
	slot.ArrayBuffer.Bytes = append([]byte(nil), bytes...)
	store.stats.LiveBytes += uint64(len(slot.ArrayBuffer.Bytes))
	return ref, nil
}

func (store *Store) DerefArrayBuffer(owner ownership.OwnerID, ref Ref) (ArrayBuffer, error) {
	if store == nil {
		return ArrayBuffer{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return ArrayBuffer{}, err
	}
	if slot.Kind != HeapArrayBuffer {
		return ArrayBuffer{}, typeError(ref, slot.Kind, HeapArrayBuffer)
	}
	return cloneArrayBuffer(slot.ArrayBuffer), nil
}

func (store *Store) ReadArrayBuffer(owner ownership.OwnerID, ref Ref, offset, length uint64) ([]byte, error) {
	if store == nil {
		return nil, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return nil, err
	}
	if slot.Kind != HeapArrayBuffer {
		return nil, typeError(ref, slot.Kind, HeapArrayBuffer)
	}
	if slot.ArrayBuffer.Detached {
		return nil, ErrDetachedBuffer
	}
	start, end, err := bufferRange(len(slot.ArrayBuffer.Bytes), offset, length)
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), slot.ArrayBuffer.Bytes[start:end]...), nil
}

func (store *Store) WriteArrayBuffer(owner ownership.OwnerID, ref Ref, offset uint64, bytes []byte) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.writeSlotLocked(owner, ref, false)
	if err != nil {
		return err
	}
	if slot.Kind != HeapArrayBuffer {
		return typeError(ref, slot.Kind, HeapArrayBuffer)
	}
	if slot.ArrayBuffer.Detached {
		return ErrDetachedBuffer
	}
	start, end, err := bufferRange(len(slot.ArrayBuffer.Bytes), offset, uint64(len(bytes)))
	if err != nil {
		return err
	}
	copy(slot.ArrayBuffer.Bytes[start:end], bytes)
	return nil
}

func (store *Store) DetachArrayBuffer(owner ownership.OwnerID, ref Ref) error {
	if store == nil {
		return fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.writeSlotLocked(owner, ref, false)
	if err != nil {
		return err
	}
	if slot.Kind != HeapArrayBuffer {
		return typeError(ref, slot.Kind, HeapArrayBuffer)
	}
	if slot.ArrayBuffer.Detached {
		return nil
	}
	store.stats.LiveBytes -= uint64(len(slot.ArrayBuffer.Bytes))
	slot.ArrayBuffer.Bytes = nil
	slot.ArrayBuffer.Detached = true
	return nil
}

func bufferRange(size int, offset, length uint64) (int, int, error) {
	end := offset + length
	if end < offset || offset > uint64(size) || end > uint64(size) {
		return 0, 0, fmt.Errorf("%w: offset=%d length=%d size=%d", ErrBufferBounds, offset, length, size)
	}
	return int(offset), int(end), nil
}
