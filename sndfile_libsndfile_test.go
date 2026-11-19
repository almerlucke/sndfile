//go:build libsndfile

package sndfile

import (
	"testing"
)

// TestUnsupportedEncodingFallsBack: encodings without a pure-Go parser must be left to libsndfile (build tag libsndfile)
func TestUnsupportedEncodingFallsBack(t *testing.T) {
	path := fixturePath("wav_alaw_mono.wav")

	if _, err := decodeFile[float64](path); err != errNoDecoder {
		t.Fatalf("got %v, want errNoDecoder", err)
	}

	if _, err := NewSoundFile[float64](path); err != nil {
		t.Fatalf("libsndfile fallback failed: %v", err)
	}
}

// TestOpenFallsBackToLibsndfile: a format without a pure-Go reader is streamed by libsndfile
func TestOpenFallsBackToLibsndfile(t *testing.T) {
	r, err := Open[float32](fixturePath("wav_alaw_mono.wav"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()

	if info := r.Info(); info.Channels != 1 || info.SampleRate != 8000 || info.Frames != 1600 {
		t.Fatalf("info %+v", info)
	}

	if err := r.SeekFrame(400); err != nil {
		t.Fatal(err)
	}

	buf := make([]float32, 400)
	if got, err := r.ReadFrames(buf); err != nil || got != 400 {
		t.Fatalf("%d frames, %v", got, err)
	}
	if got, _ := r.ReadFrames(make([]float32, 5000)); got != 800 {
		t.Fatalf("read %d frames to the end, want 800", got)
	}
	if got, err := r.ReadFrames(buf); got != 0 || err != nil {
		t.Fatalf("after the end: %d frames, %v", got, err)
	}
}

// TestFixturesMatchLibsndfile: libsndfile reads the lossless fixtures to the same samples as the pure-Go decoders
func TestFixturesMatchLibsndfile(t *testing.T) {
	for _, f := range fixtures {
		if f.lossy {
			continue
		}

		t.Run(f.file, func(t *testing.T) {
			got, err := NewSoundFile[float32](fixturePath(f.file))
			if err != nil {
				t.Fatal(err)
			}

			want, err := newSoundFileLibsndfile[float32](fixturePath(f.file))
			if err != nil {
				t.Fatal(err)
			}

			if got.NumChannels() != want.NumChannels() || got.NumFrames() != want.NumFrames() || got.SampleRate() != want.SampleRate() {
				t.Fatalf("%d channels %d frames %v Hz, libsndfile %d %d %v", got.NumChannels(), got.NumFrames(), got.SampleRate(), want.NumChannels(), want.NumFrames(), want.SampleRate())
			}
			for c := 0; c < got.NumChannels(); c++ {
				for i := range got.Buffer(c, 0) {
					if d := got.Buffer(c, 0)[i] - want.Buffer(c, 0)[i]; d > 1e-6 || d < -1e-6 {
						t.Fatalf("channel %d frame %d differs by %g", c, i, d)
					}
				}
			}
		})
	}
}
