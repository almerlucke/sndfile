//go:build !libsndfile

package sndfile

import (
	"path/filepath"
	"strings"
	"testing"
)

// without the libsndfile tag an unsupported encoding gives a clear error
func TestUnsupportedEncodingErrors(t *testing.T) {
	_, err := NewSoundFile[float64](fixturePath("wav_alaw_mono.wav"))
	if err == nil || !strings.Contains(err.Error(), "libsndfile") {
		t.Fatalf("got %v, want an unsupported format error that names the libsndfile tag", err)
	}
}

// without the libsndfile tag Open names the tag for a format that has no pure-Go reader
func TestOpenUnsupportedErrors(t *testing.T) {
	_, err := Open[float32](fixturePath("wav_alaw_mono.wav"))
	if err == nil || !strings.Contains(err.Error(), "libsndfile") {
		t.Fatalf("got %v, want an unsupported format error that names the libsndfile tag", err)
	}
}

// mp3 at 8, 11.025 and 12 kHz (mpeg 2.5) is not read by go-mp3: without libsndfile it is an error that says so, and not a panic or an empty sound
func TestMPEG25IsAnError(t *testing.T) {
	if _, err := NewSoundFile[float32](filepath.Join("testdata", "mp3_8k_st.mp3")); err == nil || !strings.Contains(err.Error(), "2.5") {
		t.Fatalf("loading gave %v, want an error about MPEG 2.5", err)
	}
	if _, err := Open[float32](filepath.Join("testdata", "mp3_8k_st.mp3")); err == nil || !strings.Contains(err.Error(), "2.5") {
		t.Fatalf("opening gave %v, want an error about MPEG 2.5", err)
	}
}
