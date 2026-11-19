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

// TestOpenFallsBackToLibsndfile: a format without a pure-Go reader is streamed by libsndfile
func TestOpenFallsBackToLibsndfile(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	path := filepath.Join(t.TempDir(), "alaw.wav")
	if out, err := exec.Command("ffmpeg", "-y", "-loglevel", "error", "-f", "lavfi", "-i",
		"sine=frequency=330:duration=0.5:sample_rate=8000", "-c:a", "pcm_alaw", path).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot make alaw: %v %s", err, out)
	}

	r, err := Open[float32](path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()

	if info := r.Info(); info.Channels != 1 || info.SampleRate != 8000 || info.Frames != 4000 {
		t.Fatalf("info %+v", info)
	}

	if err := r.SeekFrame(1000); err != nil {
		t.Fatal(err)
	}

	buf := make([]float32, 1000)
	if got, err := r.ReadFrames(buf); err != nil || got != 1000 {
		t.Fatalf("%d frames, %v", got, err)
	}
	if got, _ := r.ReadFrames(make([]float32, 5000)); got != 2000 {
		t.Fatalf("read %d frames to the end, want 2000", got)
	}
	if got, err := r.ReadFrames(buf); got != 0 || err != nil {
		t.Fatalf("after the end: %d frames, %v", got, err)
	}
}
