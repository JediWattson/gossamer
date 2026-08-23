package memory

import (
	"encoding/binary"
	"fmt"
	"math"
)

const (
	sourceLocationHeaderBytes = 5
	sourceLocationRaw         = byte(1)
	sourceLocationRuns        = byte(2)
)

// SourceLocationTable is an immutable instruction-to-source-span table. Its
// single slice header occupies the same space in Function as []SourceSpan,
// while the backing bytes use the smaller of fixed-width spans and run/delta
// encoding. Source lookup is diagnostic rather than part of ordinary bytecode
// dispatch, so compact tables decode sequentially only when an error needs a
// location.
type SourceLocationTable struct {
	data []byte
}

// NewSourceLocationTable copies and compacts spans. Consecutive equal spans
// become one run; each new start is encoded as a signed delta and End as an
// unsigned width. Fixed-width encoding is retained when it is smaller.
func NewSourceLocationTable(spans []SourceSpan) (SourceLocationTable, error) {
	if len(spans) == 0 {
		return SourceLocationTable{}, nil
	}
	if uint64(len(spans)) > math.MaxUint32 {
		return SourceLocationTable{}, fmt.Errorf("%w: too many source locations", ErrInvalidFunction)
	}
	for index, span := range spans {
		if span.End < span.Start {
			return SourceLocationTable{}, fmt.Errorf("%w: source location %d is %d..%d", ErrInvalidFunction, index, span.Start, span.End)
		}
	}

	runs := make([]byte, 0, len(spans)*3)
	previousStart := int64(0)
	for index := 0; index < len(spans); {
		span := spans[index]
		run := 1
		for index+run < len(spans) && spans[index+run] == span {
			run++
		}
		runs = binary.AppendUvarint(runs, uint64(run))
		delta := int64(span.Start) - previousStart
		runs = binary.AppendUvarint(runs, encodeSourceDelta(delta))
		runs = binary.AppendUvarint(runs, uint64(span.End-span.Start))
		previousStart = int64(span.Start)
		index += run
	}

	rawBytes := len(spans) * 8
	if len(runs) < rawBytes {
		data := make([]byte, sourceLocationHeaderBytes+len(runs))
		data[0] = sourceLocationRuns
		binary.LittleEndian.PutUint32(data[1:sourceLocationHeaderBytes], uint32(len(spans)))
		copy(data[sourceLocationHeaderBytes:], runs)
		return SourceLocationTable{data: data}, nil
	}
	data := make([]byte, sourceLocationHeaderBytes+rawBytes)
	data[0] = sourceLocationRaw
	binary.LittleEndian.PutUint32(data[1:sourceLocationHeaderBytes], uint32(len(spans)))
	for index, span := range spans {
		offset := sourceLocationHeaderBytes + index*8
		binary.LittleEndian.PutUint32(data[offset:offset+4], span.Start)
		binary.LittleEndian.PutUint32(data[offset+4:offset+8], span.End)
	}
	return SourceLocationTable{data: data}, nil
}

func encodeSourceDelta(delta int64) uint64 {
	return uint64(delta<<1) ^ uint64(delta>>63)
}

func decodeSourceDelta(value uint64) int64 {
	return int64(value>>1) ^ -int64(value&1)
}

func (table SourceLocationTable) Len() int {
	if len(table.data) < sourceLocationHeaderBytes {
		return 0
	}
	return int(binary.LittleEndian.Uint32(table.data[1:sourceLocationHeaderBytes]))
}

// At returns the exact source span for one instruction.
func (table SourceLocationTable) At(index uint32) (SourceSpan, bool) {
	if uint64(index) >= uint64(table.Len()) {
		return SourceSpan{}, false
	}
	switch table.data[0] {
	case sourceLocationRaw:
		offset := sourceLocationHeaderBytes + int(index)*8
		if offset+8 > len(table.data) {
			return SourceSpan{}, false
		}
		span := SourceSpan{
			Start: binary.LittleEndian.Uint32(table.data[offset : offset+4]),
			End:   binary.LittleEndian.Uint32(table.data[offset+4 : offset+8]),
		}
		return span, span.End >= span.Start
	case sourceLocationRuns:
		position := sourceLocationHeaderBytes
		instruction := uint64(0)
		previousStart := int64(0)
		for position < len(table.data) {
			run, read := binary.Uvarint(table.data[position:])
			if read <= 0 || run == 0 {
				return SourceSpan{}, false
			}
			position += read
			delta, read := binary.Uvarint(table.data[position:])
			if read <= 0 {
				return SourceSpan{}, false
			}
			position += read
			width, read := binary.Uvarint(table.data[position:])
			if read <= 0 {
				return SourceSpan{}, false
			}
			position += read
			start := previousStart + decodeSourceDelta(delta)
			if start < 0 || start > math.MaxUint32 || width > math.MaxUint32-uint64(start) {
				return SourceSpan{}, false
			}
			count := uint64(table.Len())
			if instruction > count || run > count-instruction {
				return SourceSpan{}, false
			}
			if uint64(index) >= instruction && uint64(index) < instruction+run {
				return SourceSpan{Start: uint32(start), End: uint32(uint64(start) + width)}, true
			}
			instruction += run
			previousStart = start
		}
	}
	return SourceSpan{}, false
}

// Spans returns a defensive decoded snapshot.
func (table SourceLocationTable) Spans() []SourceSpan {
	count := table.Len()
	if count == 0 {
		return nil
	}
	if table.data[0] == sourceLocationRaw {
		if len(table.data) != sourceLocationHeaderBytes+count*8 {
			return nil
		}
		spans := make([]SourceSpan, count)
		for index := range count {
			offset := sourceLocationHeaderBytes + index*8
			spans[index] = SourceSpan{
				Start: binary.LittleEndian.Uint32(table.data[offset : offset+4]),
				End:   binary.LittleEndian.Uint32(table.data[offset+4 : offset+8]),
			}
			if spans[index].End < spans[index].Start {
				return nil
			}
		}
		return spans
	}
	if table.data[0] != sourceLocationRuns {
		return nil
	}
	spans := make([]SourceSpan, 0, count)
	position := sourceLocationHeaderBytes
	previousStart := int64(0)
	for position < len(table.data) && len(spans) < count {
		run, read := binary.Uvarint(table.data[position:])
		if read <= 0 || run == 0 || run > uint64(count-len(spans)) {
			return nil
		}
		position += read
		delta, read := binary.Uvarint(table.data[position:])
		if read <= 0 {
			return nil
		}
		position += read
		width, read := binary.Uvarint(table.data[position:])
		if read <= 0 {
			return nil
		}
		position += read
		start := previousStart + decodeSourceDelta(delta)
		if start < 0 || start > math.MaxUint32 || width > math.MaxUint32-uint64(start) {
			return nil
		}
		span := SourceSpan{Start: uint32(start), End: uint32(uint64(start) + width)}
		for range run {
			spans = append(spans, span)
		}
		previousStart = start
	}
	if len(spans) != count || position != len(table.data) {
		return nil
	}
	return spans
}

// StorageBytes reports retained backing capacity, including the format/count
// header. Constructors and Clone always return exact-capacity storage.
func (table SourceLocationTable) StorageBytes() uint64 {
	return uint64(cap(table.data))
}

func (table SourceLocationTable) clone() SourceLocationTable {
	if len(table.data) == 0 {
		return SourceLocationTable{}
	}
	data := make([]byte, len(table.data))
	copy(data, table.data)
	return SourceLocationTable{data: data}
}

func (table SourceLocationTable) valid() bool {
	count := table.Len()
	if count == 0 {
		return len(table.data) == 0
	}
	if len(table.data) < sourceLocationHeaderBytes {
		return false
	}
	if table.data[0] == sourceLocationRaw {
		return len(table.data) == sourceLocationHeaderBytes+count*8
	}
	if table.data[0] != sourceLocationRuns {
		return false
	}
	return len(table.Spans()) == count
}

func (table SourceLocationTable) storage() []byte {
	return table.data
}
