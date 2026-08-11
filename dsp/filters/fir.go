package filters

import (
	"fmt"

	"github.com/almerlucke/sndfile/float"
)

// FIR represents a Finite Impulse Response filter taking a sinc.
// https://en.wikipedia.org/wiki/Finite_impulse_response
type FIR[T float.Float] struct {
	Sinc *Sinc
}

// LowPass applies a low pass filter using the FIR
func (f *FIR[T]) LowPass(input []T) ([]T, error) {
	return f.Convolve(input, f.Sinc.LowPassCoefs())
}

func (f *FIR[T]) HighPass(input []T) ([]T, error) {
	return f.Convolve(input, f.Sinc.HighPassCoefs())
}

// Convolve "mixes" two signals together
// kernels is the imput that is not part of our signal, it might be shorter
// than the origin signal.
func (f *FIR[T]) Convolve(input []T, kernels []float64) ([]T, error) {
	if f == nil {
		return nil, nil
	}

	if !(len(input) > len(kernels)) {
		return nil, fmt.Errorf("provided data set is not greater than the filter weights")
	}

	output := make([]T, len(input))

	for i := 0; i < len(kernels); i++ {
		var sum float64

		for j := 0; j < i; j++ {
			sum += float64(input[j]) * kernels[len(kernels)-(1+i-j)]
		}

		output[i] = T(sum)
	}

	for i := len(kernels); i < len(input); i++ {
		var sum float64

		for j := 0; j < len(kernels); j++ {
			sum += float64(input[i-j]) * kernels[j]
		}

		output[i] = T(sum)
	}

	return output, nil
}
