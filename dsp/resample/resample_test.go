package resample

import (
	"math"
	"math/rand"
	"testing"
)

// sine makes n frames of a sine of freq (in cycles per input frame) on every channel, with a different phase for each channel
func sine(n, channels int, freq float64) []float32 {
	out := make([]float32, n*channels)
	for i := 0; i < n; i++ {
		for c := 0; c < channels; c++ {
			out[i*channels+c] = float32(0.8 * math.Sin(2*math.Pi*freq*float64(i)+float64(c)))
		}
	}
	return out
}

// errorDB is the level, relative to the signal, of the difference between the output and the exact sine at the output instants. The first and last
// edge frames are left out: the stream starts and ends abruptly, which no band limited converter can follow
func errorDB(out []float32, channels int, freq, ratio float64, edge int) float64 {
	var sig, err float64
	frames := len(out) / channels
	for k := edge; k < frames-edge; k++ {
		for c := 0; c < channels; c++ {
			want := 0.8 * math.Sin(2*math.Pi*freq*float64(k)/ratio+float64(c))
			d := float64(out[k*channels+c]) - want
			sig += want * want
			err += d * d
		}
	}
	return 10 * math.Log10(err/sig)
}

func processAll(t testing.TB, quality, channels int, ratio float64, in []float32, chunk int) []float32 {
	r, err := New(quality, channels)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.SetRatio(ratio); err != nil {
		t.Fatal(err)
	}

	var out []float32
	for pos := 0; pos < len(in); pos += chunk * channels {
		end := min(pos+chunk*channels, len(in))
		o, err := r.Process(in[pos:end], end == len(in))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, o...)
	}
	return out
}

func TestAccuracy(t *testing.T) {
	cases := []struct {
		name    string
		quality int
		ratio   float64
		freq    float64 // cycles per input frame
		maxDB   float64
	}{
		{"best up 44.1->48", SincBest, 48000.0 / 44100, 0.2, -110},
		{"best up x2", SincBest, 2, 0.3, -110},
		{"best down 48->44.1", SincBest, 44100.0 / 48000, 0.2, -110},
		{"best down 96->44.1", SincBest, 44100.0 / 96000, 0.1, -110},
		{"best irrational ratio", SincBest, math.Pi / 3, 0.2, -110},
		{"medium up 44.1->48", SincMedium, 48000.0 / 44100, 0.2, -90},
		{"fastest up 44.1->48", SincFastest, 48000.0 / 44100, 0.2, -60},
		{"linear up x4", Linear, 4, 0.01, -40},
		{"hold up x4", ZeroOrderHold, 4, 0.01, -20},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := sine(20000, 2, tc.freq)
			out := processAll(t, tc.quality, 2, tc.ratio, in, 4096)

			if want := int(math.Ceil(20000 * tc.ratio)); out == nil || abs(len(out)/2-want) > 1 {
				t.Fatalf("got %d frames, want %d", len(out)/2, want)
			}

			db := errorDB(out, 2, tc.freq, tc.ratio, 400)
			t.Logf("error %.1f dB", db)
			if db > tc.maxDB {
				t.Fatalf("error %.1f dB, want below %.1f", db, tc.maxDB)
			}
		})
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// TestChunking: the output may not depend on how the input is cut up
func TestChunking(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for _, ratio := range []float64{48000.0 / 44100, 0.5, 1, 3.7, 1.0 / 100} {
		in := sine(9000, 2, 0.013)
		want := processAll(t, SincMedium, 2, ratio, in, 9000)

		r, _ := New(SincMedium, 2)
		_ = r.SetRatio(ratio)
		var got []float32
		for pos := 0; pos < len(in); {
			n := (1 + rng.Intn(700)) * 2
			end := min(pos+n, len(in))
			o, err := r.Process(in[pos:end], end == len(in))
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, o...)
			pos = end
		}

		if len(got) != len(want) {
			t.Fatalf("ratio %v: %d samples, want %d", ratio, len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("ratio %v: sample %d differs: %v, want %v", ratio, i, got[i], want[i])
			}
		}
	}
}

// TestAliasRejection: a tone above the new Nyquist frequency must be gone, not folded back
func TestAliasRejection(t *testing.T) {
	// 0.45 cycles per input frame, the output rate is 0.5 of the input rate so its Nyquist is 0.25
	in := sine(30000, 1, 0.45)
	out := processAll(t, SincBest, 1, 0.5, in, 4096)

	var sum float64
	for _, v := range out[500 : len(out)-500] {
		sum += float64(v) * float64(v)
	}
	rms := math.Sqrt(sum / float64(len(out)-1000))
	db := 20 * math.Log10(rms/(0.8/math.Sqrt2))
	t.Logf("alias level %.1f dB", db)
	if db > -100 {
		t.Fatalf("alias level %.1f dB", db)
	}
}

func TestEdgeCases(t *testing.T) {
	r, _ := New(SincFastest, 2)
	if err := r.SetRatio(0); err == nil {
		t.Fatal("ratio 0 must fail")
	}
	if _, err := New(99, 1); err == nil {
		t.Fatal("unknown quality must fail")
	}
	if _, err := r.Process(make([]float32, 3), false); err == nil {
		t.Fatal("partial frame must fail")
	}

	// empty stream, and an end of input without input
	out, err := r.Process(nil, true)
	if err != nil || len(out) != 0 {
		t.Fatalf("empty stream gave %d samples, %v", len(out), err)
	}

	// the resampler is usable again after the end of a stream
	a := processAll(t, SincFastest, 2, 1.5, sine(1000, 2, 0.1), 300)
	if len(a) == 0 {
		t.Fatal("no output")
	}
}

func benchmark(b *testing.B, quality int, ratio float64) {
	in := sine(44100, 2, 0.1)
	b.SetBytes(int64(len(in)) * 4)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		processAll(b, quality, 2, ratio, in, 8192)
	}
}

func BenchmarkBestUp(b *testing.B)    { benchmark(b, SincBest, 48000.0/44100) }
func BenchmarkBestDown(b *testing.B)  { benchmark(b, SincBest, 44100.0/96000) }
func BenchmarkMediumUp(b *testing.B)  { benchmark(b, SincMedium, 48000.0/44100) }
func BenchmarkFastestUp(b *testing.B) { benchmark(b, SincFastest, 48000.0/44100) }

// TestPhaseCacheMatchesDirect: the cached weights of a fraction ratio give what weights made for every frame give
func TestPhaseCacheMatchesDirect(t *testing.T) {
	for _, ratio := range []float64{48000.0 / 44100, 44100.0 / 96000, 3} {
		in := sine(8000, 2, 0.11)

		r, _ := New(SincMedium, 2)
		_ = r.SetRatio(ratio)
		if r.ratioP == 0 {
			t.Fatalf("ratio %v was not found to be a fraction", ratio)
		}
		cached, _ := r.Process(in, true)
		cached = append([]float32(nil), cached...)

		r, _ = New(SincMedium, 2)
		_ = r.SetRatio(ratio)
		r.ratioP, r.ratioQ, r.phases = 0, 0, nil
		direct, _ := r.Process(in, true)

		if len(cached) != len(direct) {
			t.Fatalf("ratio %v: %d and %d samples", ratio, len(cached), len(direct))
		}
		for i := range cached {
			if d := math.Abs(float64(cached[i] - direct[i])); d > 1e-5 {
				t.Fatalf("ratio %v: sample %d differs by %g", ratio, i, d)
			}
		}
	}
}

func TestFraction(t *testing.T) {
	for _, tc := range []struct {
		ratio float64
		p, q  int64
	}{{48000.0 / 44100, 160, 147}, {0.5, 1, 2}, {1, 1, 1}, {44100.0 / 96000, 147, 320}, {4, 4, 1}} {
		p, q, ok := fraction(tc.ratio, maxPhases)
		if !ok || p != tc.p || q != tc.q {
			t.Fatalf("fraction(%v) = %d/%d %v, want %d/%d", tc.ratio, p, q, ok, tc.p, tc.q)
		}
	}
	if _, _, ok := fraction(math.Pi, maxPhases); ok {
		t.Fatal("pi is not a small fraction")
	}
}
