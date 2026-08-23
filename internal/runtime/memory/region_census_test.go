package memory_test

import (
	"testing"

	"github.com/JediWattson/gossamer/internal/runtime/memory"
	"github.com/JediWattson/gossamer/internal/runtime/ownership"
)

func TestRegionCensusAttributesOnlySelectedRegion(t *testing.T) {
	t.Parallel()

	ledger := ownership.NewLedger()
	store := memory.NewStore(ledger)
	defer store.Close()
	owner := ownership.OwnerID{Kind: ownership.OwnerRealm, Value: 801}
	if err := store.RegisterOwner(owner); err != nil {
		t.Fatal(err)
	}
	first, err := store.NewRegion(owner)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.NewRegionWithLifetime(owner, ownership.OwnerTask)
	if err != nil {
		t.Fatal(err)
	}
	text, err := store.AllocString(owner, first, "strand")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AllocArray(owner, first, 3); err != nil {
		t.Fatal(err)
	}
	object, err := store.AllocObject(owner, second)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Free(owner, object); err != nil {
		t.Fatal(err)
	}

	census, err := store.RegionCensus(first)
	if err != nil {
		t.Fatal(err)
	}
	if census.ID != first || census.Owner != owner || census.Lifetime != ownership.OwnerRealm || census.State != memory.RegionPrivate {
		t.Fatalf("RegionCensus() metadata = %#v", census)
	}
	if census.Live.LiveSlots != 2 || census.Live.LiveStrings != 1 || census.Live.LiveArrays != 1 || census.Live.LiveObjects != 0 || census.Live.LiveBytes != 6 {
		t.Fatalf("RegionCensus() live = %#v", census.Live)
	}
	if census.Slots != 2 || census.FreeSlots != 0 || census.SlotCapacity < census.Slots {
		t.Fatalf("RegionCensus() storage = %#v", census)
	}

	other, err := store.RegionCensus(second)
	if err != nil {
		t.Fatal(err)
	}
	if other.Lifetime != ownership.OwnerTask || other.Live.LiveSlots != 0 || other.FreeSlots != 1 {
		t.Fatalf("second RegionCensus() = %#v", other)
	}
	if _, err := store.DerefString(owner, text); err != nil {
		t.Fatalf("census changed heap access: %v", err)
	}
	all := store.RegionCensuses()
	if len(all) != 2 || all[0].ID != first || all[1].ID != second || all[0].Live.LiveSlots != 2 || all[1].Live.LiveSlots != 0 {
		t.Fatalf("RegionCensuses() = %#v", all)
	}
}
