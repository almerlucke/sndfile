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
