package sndfile

import (
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/almerlucke/sndfile/writer/backend"
	"github.com/almerlucke/sndfile/writer/backend/aifc"
	"github.com/almerlucke/sndfile/writer/backend/wav"
)

// TestPCMMatchesLibsndfile makes files in many wav and aiff variants with ffmpeg and checks that the pure-Go parsers give what ffmpeg gives.
func TestPCMMatchesLibsndfile(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	variants := []struct {
		name string
		args []string
		ext  string
	}{
		{"wav8", []string{"-c:a", "pcm_u8"}, "wav"},
		{"wav16", []string{"-c:a", "pcm_s16le"}, "wav"},
		{"wav24", []string{"-c:a", "pcm_s24le"}, "wav"},
		{"wav32", []string{"-c:a", "pcm_s32le"}, "wav"},
		{"wavf32", []string{"-c:a", "pcm_f32le"}, "wav"},
		{"wavf64", []string{"-c:a", "pcm_f64le"}, "wav"},
		{"wav16ext", []string{"-c:a", "pcm_s16le", "-rf64", "never", "-write_bext", "1"}, "wav"},
		{"aiff8", []string{"-c:a", "pcm_s8"}, "aiff"},
		{"aiff16", []string{"-c:a", "pcm_s16be"}, "aiff"},
		{"aiff24", []string{"-c:a", "pcm_s24be"}, "aiff"},
		{"aiff32", []string{"-c:a", "pcm_s32be"}, "aiff"},
		{"aifc16le", []string{"-c:a", "pcm_s16le", "-f", "aiff"}, "aifc"},
		{"aifcf32", []string{"-c:a", "pcm_f32be", "-f", "aiff"}, "aifc"},
	}

	dir := t.TempDir()

	for _, v := range variants {
		for _, ch := range []string{"1", "2", "3"} {
			t.Run(v.name+"_ch"+ch, func(t *testing.T) {
				path := filepath.Join(dir, v.name+"_"+ch+"."+v.ext)
				args := []string{"-y", "-loglevel", "error", "-f", "lavfi", "-i",
					"sine=frequency=330:duration=0.5:sample_rate=22050", "-ac", ch}
				args = append(args, v.args...)
				args = append(args, path)

				if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
					t.Skipf("ffmpeg cannot make %s: %v %s", v.name, err, out)
				}

				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}

				var got *decoded[float64]
				if isWAV(data) {
					got, err = decodeWAV[float64](data)
				} else if isAIFF(data) {
					got, err = decodeAIFF[float64](data)
				} else {
					t.Fatalf("%s is neither wav nor aiff", path)
				}
				if err != nil {
					t.Fatal(err)
				}

				// ffmpeg decodes the same file to raw interleaved float32 as the reference
				raw, err := exec.Command("ffmpeg", "-loglevel", "error", "-i", path, "-f", "f32le", "-").Output()
				if err != nil {
					t.Fatal(err)
				}

				numCh := len(got.channels)
				if numCh != int(ch[0]-'0') || got.sampleRate != 22050 {
					t.Fatalf("got %d channels at %d Hz", numCh, got.sampleRate)
				}

				frames := len(raw) / 4 / numCh
				if len(got.channels[0]) != frames {
					t.Fatalf("%d frames, want %d", len(got.channels[0]), frames)
				}

				for i := 0; i < frames; i++ {
					for c := 0; c < numCh; c++ {
						w := float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[(i*numCh+c)*4:])))
						if d := got.channels[c][i] - w; d > 1e-6 || d < -1e-6 {
							t.Fatalf("channel %d frame %d: %v, want %v", c, i, got.channels[c][i], w)
						}
					}
				}
			})
		}
	}
}

// TestOwnWritersRoundTrip reads back what the wav and aifc backends of this module write,
func TestOwnWritersRoundTrip(t *testing.T) {
	const channels, frames = 2, 1000

	items := make([]float32, channels*frames)
	for i := range items {
		items[i] = float32(math.Sin(float64(i)*0.05)) * 0.8
	}

	dir := t.TempDir()

	writers := map[string]func(string) (backend.Backend, error){
		"own.wav":  func(p string) (backend.Backend, error) { return wav.New(p, channels, 32000) },
		"own.aifc": func(p string) (backend.Backend, error) { return aifc.New(p, channels, 32000) },
	}

	for name, mk := range writers {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name)

			w, err := mk(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := w.Write(items); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}

			got, err := decodeFile[float32](path)
			if err != nil {
				t.Fatal(err)
			}

			if len(got.channels) != channels || got.sampleRate != 32000 || len(got.channels[0]) != frames {
				t.Fatalf("got %d channels, %d Hz, %d frames", len(got.channels), got.sampleRate, len(got.channels[0]))
			}

			for i := 0; i < frames; i++ {
				for c := 0; c < channels; c++ {
					if got.channels[c][i] != items[i*channels+c] {
						t.Fatalf("frame %d channel %d: %v, want %v", i, c, got.channels[c][i], items[i*channels+c])
					}
				}
			}
		})
	}
}

// TestMalformedNeverPanics cuts valid files at every length and flips bytes: the parsers may fail, but must not panic or read out of range
func TestMalformedNeverPanics(t *testing.T) {
	dir := t.TempDir()
	var files [][]byte

	for name, mk := range map[string]func(string) (backend.Backend, error){
		"a.wav":  func(p string) (backend.Backend, error) { return wav.New(p, 2, 8000) },
		"a.aifc": func(p string) (backend.Backend, error) { return aifc.New(p, 2, 8000) },
	} {
		path := filepath.Join(dir, name)
		w, err := mk(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Write(make([]float32, 64)); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, data)
	}

	try := func(data []byte) {
		if isWAV(data) {
			_, _ = decodeWAV[float32](data)
		}
		if isAIFF(data) {
			_, _ = decodeAIFF[float32](data)
		}
	}

	for _, data := range files {
		for n := 0; n <= len(data); n++ {
			try(data[:n])
		}

		for i := range data {
			for _, v := range []byte{0x00, 0xFF, 0x7F} {
				mutated := append([]byte(nil), data...)
				mutated[i] = v
				try(mutated)
			}
		}
	}
}
