//go:build srcompare

package resample

import (
	"fmt"
	"math"
	"testing"

	"github.com/dh1tw/gosamplerate"
)

var qualityNames = map[int]string{SincBest: "best", SincMedium: "medium", SincFastest: "fastest"}

// libAll resamples a whole signal with libsamplerate, in chunks, drained properly
func libAll(t testing.TB, quality, channels int, ratio float64, in []float32, chunk int) []float32 {
	l, err := newLibSRC(quality, channels)
	if err != nil {
		t.Fatal(err)
	}
	defer l.close()

	var out []float32
	for pos := 0; pos < len(in); pos += chunk * channels {
		end := min(pos+chunk*channels, len(in))
		o, err := l.process(in[pos:end], ratio, end == len(in))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, o...)
	}
	return out
}

// wrapperAll resamples the way writer.Writer did with gosamplerate: output buffer as large as the input buffer, one call per chunk
func wrapperAll(t testing.TB, quality, channels int, ratio float64, in []float32, chunk int) []float32 {
	src, err := gosamplerate.New(quality, channels, chunk*channels)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gosamplerate.Delete(src) }()
	if err := src.SetRatio(ratio); err != nil {
		t.Fatal(err)
	}

	var out []float32
	for pos := 0; pos < len(in); pos += chunk * channels {
		end := min(pos+chunk*channels, len(in))
		o, err := src.Process(in[pos:end], ratio, end == len(in))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, o...)
	}
	return out
}

func TestCompareLibsamplerate(t *testing.T) {
	cases := []struct {
		name  string
		ratio float64
		freq  float64 // cycles per input frame
	}{
		{"44.1k -> 48k", 48000.0 / 44100, 0.2},
		{"48k -> 44.1k", 44100.0 / 48000, 0.2},
		{"44.1k -> 96k", 96000.0 / 44100, 0.3},
		{"96k -> 44.1k", 44100.0 / 96000, 0.1},
	}

	fmt.Printf("\n%-8s %-14s | %-22s | %-22s | %-22s\n", "quality", "conversion", "pure Go", "libsamplerate (direct)", "gosamplerate (writer)")
	fmt.Printf("%-8s %-14s | %-10s %-11s | %-10s %-11s | %-10s %-11s\n", "", "", "error", "frames", "error", "frames", "error", "frames")

	const frames = 44100
	for _, q := range []int{SincBest, SincMedium, SincFastest} {
		for _, tc := range cases {
			in := sine(frames, 2, tc.freq)
			want := int(math.Ceil(frames * tc.ratio))

			cell := func(out []float32) string {
				edge := 600
				db := errorDB(out, 2, tc.freq, tc.ratio, edge)
				return fmt.Sprintf("%7.1f dB  %6d/%d", db, len(out)/2, want)
			}

			pure := processAll(t, q, 2, tc.ratio, in, 8192)
			lib := libAll(t, q, 2, tc.ratio, in, 8192)
			wrap := wrapperAll(t, q, 2, tc.ratio, in, 8192)

			fmt.Printf("%-8s %-14s | %s | %s | %s\n", qualityNames[q], tc.name, cell(pure), cell(lib), cell(wrap))
		}
	}
}

func TestCompareAliasRejection(t *testing.T) {
	// 0.45 cycles per input frame, the output rate is half of the input rate so its Nyquist is 0.25: everything of it that is left is an alias
	in := sine(60000, 1, 0.45)
	level := func(out []float32) float64 {
		var sum float64
		part := out[600 : len(out)-600]
		for _, v := range part {
			sum += float64(v) * float64(v)
		}
		return 20 * math.Log10(math.Sqrt(sum/float64(len(part)))/(0.8/math.Sqrt2))
	}

	fmt.Printf("\nalias level of a 0.45 fs tone when halving the rate (lower is better)\n")
	for _, q := range []int{SincBest, SincMedium, SincFastest} {
		fmt.Printf("%-8s pure Go %7.1f dB   libsamplerate %7.1f dB\n", qualityNames[q],
			level(processAll(t, q, 1, 0.5, in, 8192)), level(libAll(t, q, 1, 0.5, in, 8192)))
	}
}

func BenchmarkCompare(b *testing.B) {
	in := sine(44100*4, 2, 0.1) // 4 seconds of stereo at 44.1k
	for _, q := range []int{SincBest, SincMedium, SincFastest} {
		for _, c := range []struct {
			name  string
			ratio float64
		}{{"up 44.1-48", 48000.0 / 44100}, {"down 96-44.1", 44100.0 / 96000}} {
			b.Run(qualityNames[q]+"/"+c.name+"/pure", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					processAll(b, q, 2, c.ratio, in, 8192)
				}
			})
			b.Run(qualityNames[q]+"/"+c.name+"/libsamplerate", func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					libAll(b, q, 2, c.ratio, in, 8192)
				}
			})
		}
	}
}
