package circuit

type InterpolationFunc func(int) uint32

func Linear(tick int) uint32 {
	return uint32(100 - tick)
}

func Logarithmic(tick int) uint32 {
	// TODO write me
	return uint32(100 - tick)
}

func Exponential(tick int) uint32 {
	// TODO write me
	return uint32(100 - tick)
}
