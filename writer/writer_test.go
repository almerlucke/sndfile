package writer_test

import (
	"math"
	"path/filepath"
	"testing"

	"github.com/almerlucke/sndfile"
	"github.com/almerlucke/sndfile/dsp/resample"
	"github.com/almerlucke/sndfile/writer"
)

// TestSampleRateConversion writes a sine in chunks, converting the rate, and reads the file back: nothing may be lost, and what is written is the sine
// at the new rate. Upsampling made more output than fitted the buffer of libsamplerate, which dropped it without telling.
func TestSampleRateConversion(t *testing.T) {
	const (
		channels = 2
		frames   = 44100
		chunk    = 512
		freq     = 0.1 // cycles per input frame
	)

	for _, tc := range []struct {
		name    string
		inRate  float64
		outRate float64
		quality int
	}{
		{"up 44.1k to 96k", 44100, 96000, resample.SincBest},
		{"up 44.1k to 48k", 44100, 48000, resample.SincMedium},
		{"down 96k to 44.1k", 96000, 44100, resample.SincFastest},
		{"same rate", 44100, 44100, resample.SincBest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out.wav")

			w, err := writer.NewWithOptions(path, writer.WAV, channels, tc.outRate, writer.Options{
				InputConverter:    writer.NewNoConverter(chunk),
				ConvertSampleRate: true,
				SrConvQuality:     tc.quality,
				InputSampleRate:   tc.inRate,
			})
			if err != nil {
				t.Fatal(err)
			}

			buf := make([]float32, chunk*channels)
			for pos := 0; pos < frames; pos += chunk {
				n := min(chunk, frames-pos)
				for i := 0; i < n; i++ {
					for c := 0; c < channels; c++ {
						buf[i*channels+c] = float32(0.8 * math.Sin(2*math.Pi*freq*float64(pos+i)+float64(c)))
					}
				}
				if err := w.Write(buf[:n*channels], pos+n == frames); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}

			sf, err := sndfile.NewSoundFile[float64](path)
			if err != nil {
				t.Fatal(err)
			}

			ratio := tc.outRate / tc.inRate
			want := int64(math.Ceil(frames * ratio))
			if sf.SampleRate() != tc.outRate || sf.NumChannels() != channels || math.Abs(float64(sf.NumFrames()-want)) > 1 {
				t.Fatalf("got %d channels at %v Hz with %d frames, want %d at %v with %d", sf.NumChannels(), sf.SampleRate(), sf.NumFrames(), channels, tc.outRate, want)
			}

			// compare with the exact sine at the new rate, without the edges where the stream starts and stops
			var sig, errSum float64
			for c := 0; c < channels; c++ {
				b := sf.Buffer(c, 0)
				for k := 1000; k < len(b)-1000; k++ {
					exact := 0.8 * math.Sin(2*math.Pi*freq*float64(k)/ratio+float64(c))
					sig += exact * exact
					errSum += (b[k] - exact) * (b[k] - exact)
				}
			}
			if db := 10 * math.Log10(errSum/sig); db > -90 {
				t.Fatalf("error %.1f dB", db)
			}
		})
	}
}

func newTestWriter(t *testing.T, path string, channels int, rate float64, opt writer.Options) *writer.Writer {
	t.Helper()

	w, err := writer.NewWithOptions(path, writer.WAV, channels, rate, opt)
	if err != nil {
		t.Fatal(err)
	}

	return w
}

func load(t *testing.T, path string) *sndfile.SoundFile[float32] {
	t.Helper()

	sf, err := sndfile.NewSoundFile[float32](path)
	if err != nil {
		t.Fatal(err)
	}

	return sf
}

// TestNormalizeUsesAbsolutePeak: a negative peak is a peak. It used to be that only the highest positive sample counted, so that a file with a
// peak of -1 and a positive peak of 0.5 came out with -2
func TestNormalizeUsesAbsolutePeak(t *testing.T) {
	path := filepath.Join(t.TempDir(), "n.wav")
	w := newTestWriter(t, path, 1, 44100, writer.Options{InputConverter: writer.NewNoConverter(4), Normalize: true})

	if err := w.Write([]float32{0.5, -1.0, 0.25, -0.5}, true); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	want := []float32{0.5, -1, 0.25, -0.5}
	for i, v := range load(t, path).Buffer(0, 0) {
		if v != want[i] {
			t.Fatalf("sample %d is %v, want %v", i, v, want[i])
		}
	}
}

// TestNormalizeAfterConversion: the peak that is normalized is that of what is written, after the sample rate conversion that can overshoot
func TestNormalizeAfterConversion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "n.wav")
	w := newTestWriter(t, path, 1, 96000, writer.Options{
		InputConverter: writer.NewNoConverter(256), ConvertSampleRate: true, InputSampleRate: 44100, Normalize: true,
	})

	// a square wave: the filter rings (overshoots) at the edges
	in := make([]float32, 8192)
	for i := range in {
		if (i/50)%2 == 0 {
			in[i] = 0.9
		} else {
			in[i] = -0.9
		}
	}
	for pos := 0; pos < len(in); pos += 256 {
		if err := w.Write(in[pos:pos+256], pos+256 == len(in)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	var peak float32
	for _, v := range load(t, path).Buffer(0, 0) {
		peak = max(peak, float32(math.Abs(float64(v))))
	}
	if math.Abs(float64(peak)-1) > 1e-6 {
		t.Fatalf("peak after normalizing is %v, want 1", peak)
	}
}

// TestCloseFinishesConversion: without endOfInput the last frames stayed in the sample rate converter, Close gives them out now
func TestCloseFinishesConversion(t *testing.T) {
	frames := func(end bool) int64 {
		path := filepath.Join(t.TempDir(), "c.wav")
		w := newTestWriter(t, path, 1, 88200, writer.Options{InputConverter: writer.NewNoConverter(1024), ConvertSampleRate: true, InputSampleRate: 44100})

		in := make([]float32, 44100)
		for i := range in {
			in[i] = float32(math.Sin(float64(i) * 0.05))
		}
		for pos := 0; pos < len(in); pos += 1024 {
			e := min(pos+1024, len(in))
			if err := w.Write(in[pos:e], end && e == len(in)); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}

		return load(t, path).NumFrames()
	}

	with, without := frames(true), frames(false)
	if with != without || with != 88200 {
		t.Fatalf("frames with endOfInput %d, without %d, want both 88200", with, without)
	}
}

// TestShortLastBlock: the last block of a render does not have to be as long as the others, and nothing of the block before it is written again
func TestShortLastBlock(t *testing.T) {
	const block = 8

	t.Run("channels", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "s.wav")
		w := newTestWriter(t, path, 2, 44100, writer.Options{InputConverter: writer.NewChannelConverter[float64](block, 2)})

		mk := func(from, n int) [][]float64 {
			l, r := make([]float64, n), make([]float64, n)
			for i := range n {
				l[i], r[i] = float64(from+i), -float64(from+i)
			}
			return [][]float64{l, r}
		}

		for _, b := range [][][]float64{mk(0, block), mk(block, 3)} {
			if err := w.Write(b, false); err != nil {
				t.Fatal(err)
			}
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}

		sf := load(t, path)
		if sf.NumFrames() != block+3 {
			t.Fatalf("%d frames, want %d", sf.NumFrames(), block+3)
		}
		for i := range sf.NumFrames() {
			if sf.Buffer(0, 0)[i] != float32(i) || sf.Buffer(1, 0)[i] != -float32(i) {
				t.Fatalf("frame %d: %v %v", i, sf.Buffer(0, 0)[i], sf.Buffer(1, 0)[i])
			}
		}
	})

	t.Run("type", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "s.wav")
		w := newTestWriter(t, path, 1, 44100, writer.Options{InputConverter: writer.NewTypeConverter[float64](4, 1)})

		if err := w.Write([]float64{1, 2, 3, 4}, false); err != nil {
			t.Fatal(err)
		}
		// used to come out as 9 9 3 4: the rest of the block before
		if err := w.Write([]float64{9, 9}, false); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}

		got := load(t, path).Buffer(0, 0)
		want := []float32{1, 2, 3, 4, 9, 9}
		if len(got) != len(want) {
			t.Fatalf("%d frames, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("sample %d is %v, want %v", i, got[i], want[i])
			}
		}
	})
}

func TestNewErrors(t *testing.T) {
	dir := t.TempDir()
	conv := writer.NewNoConverter(4)

	if _, err := writer.NewWithOptions(filepath.Join(dir, "a.wav"), writer.FileFormat(9), 1, 44100, writer.Options{InputConverter: conv}); err == nil {
		t.Error("an unknown file format must fail")
	}
	if _, err := writer.NewWithOptions(filepath.Join(dir, "b.wav"), writer.WAV, 0, 44100, writer.Options{InputConverter: conv}); err == nil {
		t.Error("0 channels must fail")
	}
	if _, err := writer.NewWithOptions(filepath.Join(dir, "c.wav"), writer.WAV, 1, 44100, writer.Options{InputConverter: conv, ConvertSampleRate: true}); err == nil {
		t.Error("converting without an input sample rate must fail")
	}
	if _, err := writer.NewWithOptions(filepath.Join(dir, "d.wav"), writer.WAV, 1, 44100, writer.Options{}); err == nil {
		t.Error("no input converter must fail")
	}
	if _, err := writer.NewWithBackend(nil, 1, 44100, writer.Options{InputConverter: conv}); err == nil {
		t.Error("no backend must fail")
	}
}

// TestWriteInputErrors: input that does not fit is an error that comes back, not a panic that ends the render
func TestWriteInputErrors(t *testing.T) {
	dir := t.TempDir()

	w := newTestWriter(t, filepath.Join(dir, "a.wav"), 2, 44100, writer.Options{InputConverter: writer.NewChannelConverter[float32](4, 2)})
	defer func() { _ = w.Close() }()

	if err := w.Write([]float32{1, 2, 3, 4}, false); err == nil {
		t.Error("input of another type must fail")
	}
	if err := w.Write([][]float32{{1, 2}, {1, 2}, {1, 2}}, false); err == nil {
		t.Error("input with another number of channels must fail")
	}

	st := newTestWriter(t, filepath.Join(dir, "b.wav"), 2, 44100, writer.Options{InputConverter: writer.NewNoConverter(4)})
	defer func() { _ = st.Close() }()

	if err := st.Write([]float32{1, 2, 3}, false); err == nil {
		t.Error("samples that are not whole frames must fail")
	}
}

func TestCloseTwiceAndWriteAfterClose(t *testing.T) {
	w := newTestWriter(t, filepath.Join(t.TempDir(), "a.wav"), 1, 44100, writer.Options{InputConverter: writer.NewNoConverter(4)})

	if err := w.Write([]float32{1, 2, 3, 4}, true); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("the second Close gave %v", err)
	}
	if err := w.Write([]float32{1}, true); err == nil {
		t.Fatal("a write after Close must fail")
	}
}
