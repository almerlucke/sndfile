package wav

import (
	"os"
	"testing"
)

// TestNewReportsHeaderWriteError: a header that cannot be written is an error. writeHeader used to return nil for every error. /dev/full takes a
// file and fails every write
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
