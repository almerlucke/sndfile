//go:build !libsndfile

package sndfile

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// without the libsndfile tag an unsupported encoding gives a clear error
func TestUnsupportedEncodingErrors(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	path := filepath.Join(t.TempDir(), "alaw.wav")
	if out, err := exec.Command("ffmpeg", "-y", "-loglevel", "error", "-f", "lavfi", "-i",
		"sine=frequency=330:duration=0.2:sample_rate=8000", "-c:a", "pcm_alaw", path).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot make alaw: %v %s", err, out)
	}

	_, err := NewSoundFile[float64](path)
	if err == nil || !strings.Contains(err.Error(), "libsndfile") {
		t.Fatalf("got %v, want an unsupported format error that names the libsndfile tag", err)
	}
}

// without the libsndfile tag Open names the tag for a format that has no pure-Go reader
func TestOpenUnsupportedErrors(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	path := filepath.Join(t.TempDir(), "alaw.wav")
	if out, err := exec.Command("ffmpeg", "-y", "-loglevel", "error", "-f", "lavfi", "-i",
		"sine=frequency=330:duration=0.2:sample_rate=8000", "-c:a", "pcm_alaw", path).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot make alaw: %v %s", err, out)
	}

	_, err := Open[float32](path)
	if err == nil || !strings.Contains(err.Error(), "libsndfile") {
		t.Fatalf("got %v, want an unsupported format error that names the libsndfile tag", err)
	}
}
