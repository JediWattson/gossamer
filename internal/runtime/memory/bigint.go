package memory

// BigInt stores a canonical unsigned big-endian magnitude plus a sign. Zero
// has an empty magnitude and is never negative.
type BigInt struct {
	Negative  bool
	Magnitude []byte
}

func cloneBigInt(value BigInt) BigInt {
	return BigInt{Negative: value.Negative, Magnitude: append([]byte(nil), value.Magnitude...)}
}

func canonicalBigInt(negative bool, magnitude []byte) BigInt {
	first := 0
	for first < len(magnitude) && magnitude[first] == 0 {
		first++
	}
	result := BigInt{Magnitude: append([]byte(nil), magnitude[first:]...)}
	result.Negative = negative && len(result.Magnitude) != 0
	return result
}
