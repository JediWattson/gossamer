package memory

import "math"

const maxDateMilliseconds = 8.64e15

type Date struct {
	Milliseconds float64
}

func timeClip(milliseconds float64) float64 {
	if math.IsNaN(milliseconds) || math.IsInf(milliseconds, 0) || math.Abs(milliseconds) > maxDateMilliseconds {
		return math.NaN()
	}
	return math.Trunc(milliseconds)
}
