package filters

import (
	"math"
	"testing"

	"github.com/almerlucke/sndfile/dsp/windows"
)

// the convolution as it was written first, one sum, one core
func convolveSlow(input []float32, kernels []float64) []float32 {
	out := make([]float32, len(input))
	for i := range kernels {
		var sum float64
		for j := 0; j < i; j++ {
			sum += float64(input[j]) * kernels[len(kernels)-(1+i-j)]
		}
		out[i] = float32(sum)
	}
	for i := len(kernels); i < len(input); i++ {
		var sum float64
		for j := range kernels {
			sum += float64(input[i-j]) * kernels[j]
		}
		out[i] = float32(sum)
	}
	return out
}

func TestConvolveIsTheSameAsTheSlowWay(t *testing.T) {
	for _, n := range []int{300, 5000, 90000} { // short ones are on one core, long ones on several
		in := make([]float32, n)
		s := uint64(7)
		for i := range in {
			s ^= s << 13
			s ^= s >> 7
			s ^= s << 17
			in[i] = float32(s%2001)/1000 - 1
		}
		for _, taps := range []int{200, 201, 7} { // an even and an odd number of taps, a short filter
			f := &FIR[float32]{Sinc: &Sinc{CutOffFreq: 8000, SamplingFreq: 44100, Taps: taps, Window: windows.Hamming}}
			got, err := f.LowPass(in)
			if n <= taps {
				if err == nil {
					t.Errorf("a signal shorter than the filter (%d, %d taps) is not refused", n, taps)
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			want := convolveSlow(in, f.Sinc.LowPassCoefs())
			for i := range want {
				if math.Abs(float64(got[i]-want[i])) > 1e-6 {
					t.Fatalf("%d samples, %d taps: sample %d is %v, the slow way gives %v", n, taps, i, got[i], want[i])
				}
			}
		}
	}
}
