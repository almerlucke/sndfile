package sndfile

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestDecodeFormats generates a 1s 440Hz stereo sine with ffmpeg in each
// format and checks the decoded result. Skipped when ffmpeg is unavailable.
func TestDecodeFormats(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	dir := t.TempDir()

	for _, ext := range []string{"wav", "flac", "ogg", "mp3"} {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(dir, "tone."+ext)
			cmd := exec.Command("ffmpeg", "-y", "-loglevel", "error", "-f", "lavfi",
				"-i", "sine=frequency=440:duration=1:sample_rate=44100", "-ac", "2", path)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Skipf("ffmpeg cannot encode %s: %v %s", ext, err, out)
			}

			if _, err := os.Stat(path); err != nil {
				t.Fatal(err)
			}

			sf, err := NewSoundFile[float64](path)
			if err != nil {
				t.Fatal(err)
			}

			if sf.NumChannels() != 2 || sf.SampleRate() != 44100 {
				t.Fatalf("got %d channels at %v Hz", sf.NumChannels(), sf.SampleRate())
			}

			// lossy codecs add padding, allow 10% deviation
			if math.Abs(sf.Duration()-1) > 0.1 {
				t.Fatalf("duration %v", sf.Duration())
			}

			peak := 0.0
			for _, v := range sf.Buffer(0, 0) {
				peak = math.Max(peak, math.Abs(v))
			}

			if peak < 0.05 || peak > 1.1 {
				t.Fatalf("unexpected peak %v", peak)
			}
		})
	}
}
