package memory

import (
	"math"
	"testing"
	"unsafe"
)

func TestSourceLocationTableRoundTripsRunsAndBackwardDeltas(t *testing.T) {
	t.Parallel()

	spans := []SourceSpan{
		{}, {}, {},
		{Start: 100, End: 120}, {Start: 100, End: 120},
		{Start: 12, End: 13},
		{Start: math.MaxUint32 - 4, End: math.MaxUint32},
	}
	table, err := NewSourceLocationTable(spans)
	if err != nil {
		t.Fatal(err)
	}
	if unsafe.Sizeof(table) != unsafe.Sizeof([]SourceSpan(nil)) {
		t.Fatalf("SourceLocationTable size = %d, slice header = %d", unsafe.Sizeof(table), unsafe.Sizeof([]SourceSpan(nil)))
	}
	if table.Len() != len(spans) || !table.valid() {
		t.Fatalf("table length/validity = %d/%t", table.Len(), table.valid())
	}
	for index, want := range spans {
		got, found := table.At(uint32(index))
		if !found || got != want {
			t.Fatalf("location %d = %#v, %t; want %#v", index, got, found, want)
		}
	}
	if _, found := table.At(uint32(len(spans))); found {
		t.Fatal("out-of-range location resolved")
	}
	decoded := table.Spans()
	if len(decoded) != len(spans) {
		t.Fatalf("decoded length = %d", len(decoded))
	}
	decoded[0] = SourceSpan{Start: 8, End: 9}
	if got, _ := table.At(0); got != spans[0] {
		t.Fatalf("decoded snapshot mutated table: %#v", got)
	}
}

func TestSourceLocationTableUsesBoundedAdaptiveStorage(t *testing.T) {
	t.Parallel()

	repeated := make([]SourceSpan, 1024)
	for index := range repeated {
		repeated[index] = SourceSpan{Start: 40, End: 80}
	}
	compact, err := NewSourceLocationTable(repeated)
	if err != nil {
		t.Fatal(err)
	}
	if compact.StorageBytes() >= uint64(len(repeated))*8/100 {
		t.Fatalf("repeated table retained %d bytes for %d raw bytes", compact.StorageBytes(), len(repeated)*8)
	}

	wide := []SourceSpan{
		{Start: 0, End: math.MaxUint32},
		{Start: math.MaxUint32, End: math.MaxUint32},
		{Start: 1, End: math.MaxUint32},
	}
	bounded, err := NewSourceLocationTable(wide)
	if err != nil {
		t.Fatal(err)
	}
	maximum := uint64(sourceLocationHeaderBytes + len(wide)*8)
	if bounded.StorageBytes() > maximum {
		t.Fatalf("adaptive table retained %d bytes, maximum raw form %d", bounded.StorageBytes(), maximum)
	}
	if got := bounded.Spans(); len(got) != len(wide) || got[0] != wide[0] || got[2] != wide[2] {
		t.Fatalf("bounded table decoded %#v", got)
	}
}

func TestSourceLocationTableRejectsInvalidSpans(t *testing.T) {
	t.Parallel()

	if _, err := NewSourceLocationTable([]SourceSpan{{Start: 9, End: 2}}); err == nil {
		t.Fatal("invalid source span was accepted")
	}
}
