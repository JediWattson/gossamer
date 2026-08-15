package memory

import (
	"fmt"
	"math/big"

	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func (store *Store) AllocBigInt(owner ownership.OwnerID, regionID RegionID, negative bool, magnitude []byte) (Ref, error) {
	if store == nil {
		return Ref{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	ref, err := store.allocKindLocked(owner, regionID, HeapBigInt, false)
	if err != nil {
		return Ref{}, err
	}
	_, slot, _ := store.slotLocked(ref)
	slot.BigInt = canonicalBigInt(negative, magnitude)
	store.stats.LiveBytes += uint64(len(slot.BigInt.Magnitude))
	return ref, nil
}

// ParseBigInt accepts bases 2 through 36, or base 0 for Go-style prefixes.
func (store *Store) ParseBigInt(owner ownership.OwnerID, regionID RegionID, text string, base int) (Ref, error) {
	if base != 0 && (base < 2 || base > 36) {
		return Ref{}, fmt.Errorf("%w: base %d", ErrInvalidBigInt, base)
	}
	parsed, ok := new(big.Int).SetString(text, base)
	if !ok {
		return Ref{}, fmt.Errorf("%w: %q", ErrInvalidBigInt, text)
	}
	return store.AllocBigInt(owner, regionID, parsed.Sign() < 0, parsed.Bytes())
}

func (store *Store) DerefBigInt(owner ownership.OwnerID, ref Ref) (BigInt, error) {
	if store == nil {
		return BigInt{}, fmt.Errorf("memory: nil store")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	_, slot, err := store.readSlotLocked(owner, ref)
	if err != nil {
		return BigInt{}, err
	}
	if slot.Kind != HeapBigInt {
		return BigInt{}, typeError(ref, slot.Kind, HeapBigInt)
	}
	return cloneBigInt(slot.BigInt), nil
}

func (store *Store) BigIntText(owner ownership.OwnerID, ref Ref, base int) (string, error) {
	if base < 2 || base > 36 {
		return "", fmt.Errorf("%w: base %d", ErrInvalidBigInt, base)
	}
	value, err := store.DerefBigInt(owner, ref)
	if err != nil {
		return "", err
	}
	integer := new(big.Int).SetBytes(value.Magnitude)
	if value.Negative {
		integer.Neg(integer)
	}
	return integer.Text(base), nil
}
