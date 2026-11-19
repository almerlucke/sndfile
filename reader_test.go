package sndfile

import (
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

type readerVariant struct {
	name     string
	ext      string
	channels int
	args     []string
	exact    bool // the seek must give the same samples as reading on from the start
}

var readerVariants = []readerVariant{
	{"wav16 mono", "wav", 1, []string{"-c:a", "pcm_s16le"}, true},
	{"wav16 stereo", "wav", 2, []string{"-c:a", "pcm_s16le"}, true},
	{"wav24 3ch", "wav", 3, []string{"-c:a", "pcm_s24le"}, true},
	{"wav32", "wav", 2, []string{"-c:a", "pcm_s32le"}, true},
	{"wavf32", "wav", 2, []string{"-c:a", "pcm_f32le"}, true},
	{"wavf64", "wav", 2, []string{"-c:a", "pcm_f64le"}, true},
	{"wav8", "wav", 2, []string{"-c:a", "pcm_u8"}, true},
	{"aiff16", "aiff", 2, []string{"-c:a", "pcm_s16be"}, true},
	{"aiff24", "aiff", 2, []string{"-c:a", "pcm_s24be"}, true},
	{"aifc sowt", "aifc", 2, []string{"-c:a", "pcm_s16le", "-f", "aiff"}, true},
	{"flac stereo", "flac", 2, nil, true},
	{"flac mono", "flac", 1, nil, true},
	{"ogg stereo", "ogg", 2, nil, false},
	{"mp3 stereo", "mp3", 2, nil, false},
}

// makeReaderFile makes 2 seconds of a chirp, a different one on every channel
func makeReaderFile(t *testing.T, dir string, v readerVariant) string {
	t.Helper()

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not available")
	}

	expr := ""
	for c := 0; c < v.channels; c++ {
		if c > 0 {
			expr += "|"
		}
		expr += "0.6*sin(2*PI*(200+" + string(rune('3'+c)) + "00*t)*t)"
	}

	path := filepath.Join(dir, filepath.Base(v.name)+"."+v.ext)
	args := []string{"-y", "-loglevel", "error", "-f", "lavfi", "-i", "aevalsrc=" + expr + ":s=22050:d=2"}
	args = append(args, v.args...)
	args = append(args, path)

	if out, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot make %s: %v %s", v.name, err, out)
	}

	return path
}

// TestReaderReadsLikeLoader: reading a file in pieces of every size gives what the loader gives
func TestReaderReadsLikeLoader(t *testing.T) {
	dir := t.TempDir()

	for _, v := range readerVariants {
		t.Run(v.name, func(t *testing.T) {
			path := makeReaderFile(t, dir, v)

			want, err := NewSoundFile[float32](path)
			if err != nil {
				t.Fatal(err)
			}

			r, err := Open[float32](path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = r.Close() }()

			info := r.Info()
			if info.Channels != want.NumChannels() || info.SampleRate != want.SampleRate() || info.Frames != want.NumFrames() {
				t.Fatalf("info %+v, loader %d channels %v Hz %d frames", info, want.NumChannels(), want.SampleRate(), want.NumFrames())
			}

			sizes := []int{1, 7, 1000, 4096, 333}
			buf := make([]float32, 4096*info.Channels)
			var pos int64

			for i := 0; ; i++ {
				n := sizes[i%len(sizes)]
				got, err := r.ReadFrames(buf[:n*info.Channels])
				if err != nil {
					t.Fatal(err)
				}
				if got == 0 {
					break
				}

				for f := int64(0); f < got; f++ {
					for c := 0; c < info.Channels; c++ {
						if buf[int(f)*info.Channels+c] != want.Buffer(c, 0)[pos+f] {
							t.Fatalf("frame %d channel %d: %v, loader %v", pos+f, c, buf[int(f)*info.Channels+c], want.Buffer(c, 0)[pos+f])
						}
					}
				}

				pos += got
			}

			if pos != info.Frames {
				t.Fatalf("read %d frames of %d", pos, info.Frames)
			}

			// at the end there is nothing more, and no error
			if got, err := r.ReadFrames(buf); got != 0 || err != nil {
				t.Fatalf("after the end: %d frames, %v", got, err)
			}
		})
	}
}

// TestReaderSeek: seeking to any frame and reading gives what the loader has there, also to the end and over and over (a loop)
func TestReaderSeek(t *testing.T) {
	dir := t.TempDir()
	rng := rand.New(rand.NewSource(7))

	for _, v := range readerVariants {
		t.Run(v.name, func(t *testing.T) {
			path := makeReaderFile(t, dir, v)

			want, err := NewSoundFile[float32](path)
			if err != nil {
				t.Fatal(err)
			}

			r, err := Open[float32](path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = r.Close() }()

			frames := r.Info().Frames
			ch := r.Info().Channels
			buf := make([]float32, 1500*ch)

			positions := []int64{0, 1, frames - 1, frames, frames / 2, 4095, 4096, 4097}
			for range 40 {
				positions = append(positions, rng.Int63n(frames))
			}

			var worst float64
			for _, pos := range positions {
				if err := r.SeekFrame(pos); err != nil {
					t.Fatalf("seek %d: %v", pos, err)
				}

				got, err := r.ReadFrames(buf)
				if err != nil {
					t.Fatal(err)
				}
				if wantN := min(1500, frames-pos); got != wantN {
					t.Fatalf("seek %d: read %d frames, want %d", pos, got, wantN)
				}

				for f := int64(0); f < got; f++ {
					for c := 0; c < ch; c++ {
						d := math.Abs(float64(buf[int(f)*ch+c] - want.Buffer(c, 0)[pos+f]))
						worst = max(worst, d)
						if v.exact && d != 0 {
							t.Fatalf("seek %d, frame %d channel %d differs by %g", pos, f, c, d)
						}
					}
				}
			}

			t.Logf("worst difference after a seek: %g", worst)
			if worst > 1e-3 {
				t.Fatalf("worst difference after a seek: %g", worst)
			}

			// a position that is not in the file
			if err := r.SeekFrame(frames + 1); err == nil {
				t.Fatal("seek past the end must fail")
			}
			if err := r.SeekFrame(-1); err == nil {
				t.Fatal("seek before the start must fail")
			}
		})
	}
}

// TestReaderFloat64 reads into float64 as well
func TestReaderFloat64(t *testing.T) {
	path := makeReaderFile(t, t.TempDir(), readerVariants[1])

	want, err := NewSoundFile[float64](path)
	if err != nil {
		t.Fatal(err)
	}

	r, err := Open[float64](path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()

	buf := make([]float64, 2*100)
	if got, err := r.ReadFrames(buf); err != nil || got != 100 {
		t.Fatalf("%d frames, %v", got, err)
	}
	for f := 0; f < 100; f++ {
		if buf[2*f] != want.Buffer(0, 0)[f] || buf[2*f+1] != want.Buffer(1, 0)[f] {
			t.Fatalf("frame %d differs", f)
		}
	}
}

// TestReaderTruncatedFile: a file that is shorter than its header says gives the frames that there are, and no error
func TestReaderTruncatedFile(t *testing.T) {
	path := makeReaderFile(t, t.TempDir(), readerVariants[1])

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	cut := filepath.Join(t.TempDir(), "cut.wav")
	if err := os.WriteFile(cut, data[:len(data)/2], 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := Open[float32](cut)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()

	buf := make([]float32, 2*1024)
	var total int64
	for {
		got, err := r.ReadFrames(buf)
		if err != nil {
			t.Fatal(err)
		}
		if got == 0 {
			break
		}
		total += got
	}

	if total != r.Info().Frames || total == 0 {
		t.Fatalf("read %d frames, the file has %d", total, r.Info().Frames)
	}
}

func TestOpenErrors(t *testing.T) {
	if _, err := Open[float32](filepath.Join(t.TempDir(), "missing.wav")); err == nil {
		t.Fatal("a missing file must fail")
	}

	junk := filepath.Join(t.TempDir(), "junk.wav")
	if err := os.WriteFile(junk, []byte("this is not a sound file at all, not even close"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open[float32](junk); err == nil {
		t.Fatal("junk must fail")
	}
}
