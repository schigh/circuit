package circuit

import "math/rand/v2"

/*
These curves were generated using gonum.org/v1/plot/tools/bezier
*/

// EstimationFunc takes in a number within range [1 - 100] and returns a probability that a
// request should be blocked based on that number.
// The tick value is calculated from elapsed time: tick = elapsed * 100 / backoff.
// Tick 1 represents the start of the backoff period; tick 100 represents the end.
// The function is called lazily on each request during the throttled state.
type EstimationFunc func(int) uint32

var logCurve = []uint32{
	100, 99, 99, 99, 99, 99, 99, 99, 99, 99,
	99, 99, 99, 99, 99, 99, 99, 99, 99, 99,
	99, 99, 98, 98, 98, 98, 98, 97, 97, 97,
	97, 96, 96, 96, 95, 95, 95, 94, 94, 93,
	93, 92, 92, 91, 91, 90, 89, 89, 88, 87,
	87, 86, 85, 84, 83, 82, 81, 80, 79, 78,
	77, 76, 75, 74, 72, 71, 70, 69, 67, 66,
	64, 63, 61, 59, 58, 56, 54, 52, 51, 49,
	47, 45, 43, 41, 38, 36, 34, 32, 29, 27,
	24, 22, 19, 17, 14, 11, 8, 5, 2, 0,
}

var expCurve = []uint32{
	100, 97, 94, 91, 88, 85, 82, 80, 77, 75,
	72, 70, 67, 65, 63, 61, 58, 56, 54, 52,
	50, 48, 47, 45, 43, 41, 40, 38, 36, 35,
	33, 32, 30, 29, 28, 27, 25, 24, 23, 22,
	21, 20, 19, 18, 17, 16, 15, 14, 13, 12,
	12, 11, 10, 10, 9, 8, 8, 7, 7, 6,
	6, 5, 5, 4, 4, 4, 3, 3, 3, 2,
	2, 2, 2, 1, 1, 1, 1, 1, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
}

var easeInOutCurve = []uint32{
	100, 99, 99, 99, 99, 99, 98, 98, 98, 97,
	97, 96, 95, 95, 94, 93, 93, 92, 91, 90,
	89, 88, 87, 86, 85, 84, 82, 81, 80, 79,
	78, 76, 75, 74, 72, 71, 69, 68, 67, 65,
	64, 62, 61, 59, 58, 56, 55, 53, 52, 50,
	49, 47, 46, 44, 43, 41, 40, 38, 37, 35,
	34, 32, 31, 30, 28, 27, 25, 24, 23, 21,
	20, 19, 18, 17, 15, 14, 13, 12, 11, 10,
	9, 8, 7, 6, 6, 5, 4, 4, 3, 2,
	2, 1, 1, 1, 0, 0, 0, 0, 0, 0,
}

// Linear backoff will return a probability directly
// proportional to the current tick
func Linear(tick int) uint32 {
	return uint32(100 - tick)
}

// Logarithmic backoff will block most initial requests and
// increase the rate of passes at a similar rate after the
// middle point in the curve is reached
func Logarithmic(tick int) uint32 {
	return logCurve[tick-1]
}

// Exponential backoff will reduce the number of blocks
// drastically at first, gradually slowing the rate
func Exponential(tick int) uint32 {
	return expCurve[tick-1]
}

// EaseInOut will block most requests initially, then pass at
// a steep rate, eventually slowing down the pass rate
func EaseInOut(tick int) uint32 {
	return easeInOutCurve[tick-1]
}

// JitteredLinear is like Linear but adds random jitter of +/- 5
// to reduce thundering herd effects during recovery.
func JitteredLinear(tick int) uint32 {
	base := 100 - tick
	jitter := int(rand.IntN(11)) - 5 // [-5, 5]
	result := base + jitter
	if result > 100 {
		return 100
	}
	if result < 0 {
		return 0
	}
	return uint32(result)
}
