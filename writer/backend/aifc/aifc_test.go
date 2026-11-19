package aifc

import (
	"os"
	"testing"
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
