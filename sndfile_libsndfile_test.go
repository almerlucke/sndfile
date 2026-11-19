//go:build libsndfile

package sndfile

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestUnsupportedEncodingFallsBack: encodings without a pure-Go parser must be left to libsndfile (build tag libsndfile)
func TestUnsupportedEncodingFallsBack(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	path := filepath.Join(t.TempDir(), "alaw.wav")
	if out, err := exec.Command("ffmpeg", "-y", "-loglevel", "error", "-f", "lavfi", "-i",
		"sine=frequency=330:duration=0.2:sample_rate=8000", "-c:a", "pcm_alaw", path).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot make alaw: %v %s", err, out)
	}

	if _, err := decodeFile[float64](path); err != errNoDecoder {
		t.Fatalf("got %v, want errNoDecoder", err)
	}

	if _, err := NewSoundFile[float64](path); err != nil {
		t.Fatalf("libsndfile fallback failed: %v", err)
	}
}
