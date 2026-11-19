package filters

import (
	"fmt"
	"runtime"
	"sync"

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

	// the rest: a windowed sinc is symmetric, so the two samples that have the same weight are added first (half the multiplications), four sums run
	// side by side (a sum that waits for the one before it is slow), and the output is cut in pieces for the cores
	n := len(kernels)
	sym := true
	for j := 0; j < n/2; j++ {
		if kernels[j] != kernels[n-1-j] {
			sym = false
			break
		}
	}
	work := func(from, to int) {
		for i := from; i < to; i++ {
			x := input[i-n+1 : i+1] // x[n-1-j] is input[i-j]
			var s0, s1, s2, s3 float64
			if sym {
				h := n / 2
				j := 0
				for ; j+4 <= h; j += 4 {
					s0 += (float64(x[n-1-j]) + float64(x[j])) * kernels[j]
					s1 += (float64(x[n-2-j]) + float64(x[j+1])) * kernels[j+1]
					s2 += (float64(x[n-3-j]) + float64(x[j+2])) * kernels[j+2]
					s3 += (float64(x[n-4-j]) + float64(x[j+3])) * kernels[j+3]
				}
				for ; j < h; j++ {
					s0 += (float64(x[n-1-j]) + float64(x[j])) * kernels[j]
				}
				if n%2 == 1 {
					s1 += float64(x[h]) * kernels[h]
				}
			} else {
				j := 0
				for ; j+4 <= n; j += 4 {
					s0 += float64(x[n-1-j]) * kernels[j]
					s1 += float64(x[n-2-j]) * kernels[j+1]
					s2 += float64(x[n-3-j]) * kernels[j+2]
					s3 += float64(x[n-4-j]) * kernels[j+3]
				}
				for ; j < n; j++ {
					s0 += float64(x[n-1-j]) * kernels[j]
				}
			}
			output[i] = T(s0 + s1 + s2 + s3)
		}
	}
	total := len(input) - n
	workers := min(runtime.NumCPU(), max(1, total/20000)) // a short signal is not worth the goroutines
	if workers == 1 {
		work(n, len(input))
		return output, nil
	}
	var wg sync.WaitGroup
	chunk := (total + workers - 1) / workers
	for w := 0; w < workers; w++ {
		from := n + w*chunk
		to := min(from+chunk, len(input))
		if from >= to {
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			work(from, to)
		}()
	}
	wg.Wait()
	return output, nil
}
