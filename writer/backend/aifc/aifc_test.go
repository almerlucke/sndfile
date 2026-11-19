package aifc

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/almerlucke/sndfile/writer/backend"
)

func TestNewReportsHeaderWriteError(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("no /dev/full")
	}

	w, err := New("/dev/full", 2, 44100)
	if err == nil {
		_ = w.Close()
		t.Fatal("writing the header to a full disk must fail")
	}
}

func newTemp(t *testing.T, channels int) *AIFC {
	t.Helper()

	w, err := New(filepath.Join(t.TempDir(), "w.aif"), channels, 44100)
	if err != nil {
		t.Fatal(err)
	}

	return w
}

// TestWriteStopsAtFourGiB: the sizes in an aifc header are 32 bits, a file that would not fit is an error and not a header that wrapped around
func TestWriteStopsAtFourGiB(t *testing.T) {
	w := newTemp(t, 2)
	w.numSampleFrames = maxSampleBytes/8 - 1

	if err := w.Write([]float32{0, 0}); err != nil {
		t.Fatalf("the last frame that fits: %v", err)
	}
	before := w.numSampleFrames
	if err := w.Write([]float32{0, 0}); !errors.Is(err, backend.ErrTooLarge) {
		t.Fatalf("a frame too many gave %v, want ErrTooLarge", err)
	}
	if w.numSampleFrames != before {
		t.Fatal("a refused write was counted")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestWriteRejectsPartialFrame(t *testing.T) {
	w := newTemp(t, 2)
	defer func() { _ = w.Close() }()

	if err := w.Write([]float32{0, 0, 0}); err == nil {
		t.Fatal("3 samples are no whole frames of 2 channels")
	}
}

func TestNewValidates(t *testing.T) {
	dir := t.TempDir()

	for _, tc := range []struct {
		channels int
		rate     float64
	}{{0, 44100}, {-1, 44100}, {40000, 44100}, {2, 0}, {2, -5}} {
		if w, err := New(filepath.Join(dir, "x.aif"), tc.channels, tc.rate); err == nil {
			_ = w.Close()
			t.Errorf("%d channels at %v Hz must fail", tc.channels, tc.rate)
		}
	}
}
