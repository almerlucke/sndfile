package sndfile

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// The files in testdata are made by testdata/gen.sh: small sound files in many formats, and next to each of them what ffmpeg decodes from it (raw
// interleaved float32, .f32). The tests here use them, so they run without ffmpeg.

type fixture struct {
	file     string
	channels int
	rate     int
	// lossy files (mp3, ogg) do not decode to the same samples as ffmpeg gives: they are compared after finding the delay between the two
	lossy bool
	// gapless files (mp3 with the tag of the encoder) are cut as ffmpeg cuts them: they start where the sound starts, and are as long as it
	gapless bool
}

var fixtures = []fixture{
	{"wav_u8_st.wav", 2, 8000, false, false},
	{"wav_s16_mono.wav", 1, 8000, false, false},
	{"wav_s16_st.wav", 2, 8000, false, false},
	{"wav_s24_3ch.wav", 3, 8000, false, false},
	{"wav_s32_st.wav", 2, 8000, false, false},
	{"wav_f32_st.wav", 2, 8000, false, false},
	{"wav_f64_st.wav", 2, 8000, false, false},
	{"aiff_s8_st.aiff", 2, 8000, false, false},
	{"aiff_s16_st.aiff", 2, 8000, false, false},
	{"aiff_s24_st.aiff", 2, 8000, false, false},
	{"aiff_s32_st.aiff", 2, 8000, false, false},
	{"aifc_sowt_st.aifc", 2, 8000, false, false},
	{"aifc_fl32_st.aifc", 2, 8000, false, false},
	{"flac_s16_st.flac", 2, 8000, false, false},
	{"flac_s16_mono.flac", 1, 8000, false, false},
	{"flac_s24_st.flac", 2, 8000, false, false},
	{"ogg_st.ogg", 2, 8000, true, false},
	{"mp3_22k_st.mp3", 2, 22050, true, true},
	{"mp3_44k_st.mp3", 2, 44100, true, true},
}

func fixturePath(name string) string { return filepath.Join("testdata", name) }

// fixtureRef is what ffmpeg decodes from a fixture, per channel
func fixtureRef(t testing.TB, f fixture) [][]float32 {
	t.Helper()

	raw, err := os.ReadFile(fixturePath(f.file[:len(f.file)-len(filepath.Ext(f.file))] + ".f32"))
	if err != nil {
		t.Fatal(err)
	}

	frames := len(raw) / 4 / f.channels
	ref := make([][]float32, f.channels)
	for c := range ref {
		ref[c] = make([]float32, frames)
		for i := range ref[c] {
			ref[c][i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[(i*f.channels+c)*4:]))
		}
	}

	return ref
}

// bestLag finds how many frames got is behind ref (decoders of mp3 do not leave out the delay of the encoder, ffmpeg does), by the lowest error over a
// window of ref. It gives the lag and the relative rms error at that lag
func bestLag(ref, got []float32) (int, float64) {
	const start, window, maxLag = 300, 900, 3000

	best, bestLag := math.Inf(1), 0
	for lag := -maxLag; lag <= maxLag; lag++ {
		var e, s float64

		for i := start; i < start+window; i++ {
			j := i + lag
			if j < 0 || j >= len(got) {
				e, s = math.Inf(1), 1
				break
			}
			d := float64(ref[i]) - float64(got[j])
			e += d * d
			s += float64(ref[i]) * float64(ref[i])
		}

		if e/s < best {
			best, bestLag = e/s, lag
		}
	}

	return bestLag, math.Sqrt(best)
}

func TestFixturesDecode(t *testing.T) {
	for _, f := range fixtures {
		t.Run(f.file, func(t *testing.T) {
			sf, err := NewSoundFile[float32](fixturePath(f.file))
			if err != nil {
				t.Fatal(err)
			}

			if sf.NumChannels() != f.channels || sf.SampleRate() != float64(f.rate) {
				t.Fatalf("%d channels at %v Hz, want %d at %d", sf.NumChannels(), sf.SampleRate(), f.channels, f.rate)
			}

			ref := fixtureRef(t, f)

			if f.lossy {
				for c := range ref {
					lag, rel := bestLag(ref[c], sf.Buffer(c, 0))
					t.Logf("channel %d: %d frames of delay, relative error %.1e", c, lag, rel)
					if rel > 1e-3 {
						t.Fatalf("channel %d does not follow the reference: relative error %g at a lag of %d", c, rel, lag)
					}
					if f.gapless && lag != 0 {
						t.Fatalf("channel %d starts %d frames late", c, lag)
					}
				}

				if f.gapless && int(sf.NumFrames()) != len(ref[0]) {
					t.Fatalf("%d frames, want the %d of the sound", sf.NumFrames(), len(ref[0]))
				}

				return
			}

			if int(sf.NumFrames()) != len(ref[0]) {
				t.Fatalf("%d frames, want %d", sf.NumFrames(), len(ref[0]))
			}

			for c := range ref {
				for i, want := range ref[c] {
					if d := math.Abs(float64(sf.Buffer(c, 0)[i] - want)); d > 1e-6 {
						t.Fatalf("channel %d frame %d: %v, want %v", c, i, sf.Buffer(c, 0)[i], want)
					}
				}
			}
		})
	}
}

// TestFixturesReader: the reader gives what the loader gives, also after seeks
func TestFixturesReader(t *testing.T) {
	for _, f := range fixtures {
		t.Run(f.file, func(t *testing.T) {
			want, err := NewSoundFile[float32](fixturePath(f.file))
			if err != nil {
				t.Fatal(err)
			}

			r, err := Open[float32](fixturePath(f.file))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = r.Close() }()

			if info := r.Info(); info.Channels != want.NumChannels() || info.SampleRate != want.SampleRate() || info.Frames != want.NumFrames() {
				t.Fatalf("info %+v, loader %d channels %v Hz %d frames", info, want.NumChannels(), want.SampleRate(), want.NumFrames())
			}

			ch := want.NumChannels()
			buf := make([]float32, 333*ch)
			var pos int64

			for {
				got, err := r.ReadFrames(buf)
				if err != nil {
					t.Fatal(err)
				}
				if got == 0 {
					break
				}
				for i := int64(0); i < got; i++ {
					for c := 0; c < ch; c++ {
						if buf[int(i)*ch+c] != want.Buffer(c, 0)[pos+i] {
							t.Fatalf("frame %d channel %d differs", pos+i, c)
						}
					}
				}
				pos += got
			}

			if pos != want.NumFrames() {
				t.Fatalf("read %d frames of %d", pos, want.NumFrames())
			}

			for _, at := range []int64{0, 1, 577, want.NumFrames() / 2, want.NumFrames() - 1, want.NumFrames()} {
				if err := r.SeekFrame(at); err != nil {
					t.Fatalf("seek %d: %v", at, err)
				}

				got, err := r.ReadFrames(buf)
				if err != nil {
					t.Fatal(err)
				}
				if wantN := min(333, want.NumFrames()-at); got != wantN {
					t.Fatalf("seek %d: read %d frames, want %d", at, got, wantN)
				}
				for i := int64(0); i < got; i++ {
					for c := 0; c < ch; c++ {
						if buf[int(i)*ch+c] != want.Buffer(c, 0)[at+i] {
							t.Fatalf("seek %d: frame %d channel %d differs", at, i, c)
						}
					}
				}
			}
		})
	}
}

// patchSizes makes the placeholder sizes of a writer that did not finish in a copy of a fixture: they are 0 in the header, the sound is all there
func patchSizes(t *testing.T, name string, offsetsOf func(data []byte) []int, order binary.ByteOrder) string {
	t.Helper()

	data, err := os.ReadFile(fixturePath(name))
	if err != nil {
		t.Fatal(err)
	}

	for _, off := range offsetsOf(data) {
		order.PutUint32(data[off:], 0)
	}

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

func indexOf(data []byte, id string, from int) int {
	for i := from; i+4 <= len(data); i++ {
		if string(data[i:i+4]) == id {
			return i
		}
	}

	return -1
}

// TestFixturesWithPlaceholderSizes: files of a writer that stopped before its sizes were written still give all of their samples
func TestFixturesWithPlaceholderSizes(t *testing.T) {
	cases := []struct {
		fix     fixture
		offsets func(data []byte) []int
		order   binary.ByteOrder
	}{
		// wav: the size of the RIFF and of the data chunk
		{fixtures[2], func(d []byte) []int { return []int{4, indexOf(d, "data", 12) + 4} }, binary.LittleEndian},
		{fixtures[5], func(d []byte) []int { return []int{4, indexOf(d, "data", 12) + 4} }, binary.LittleEndian},
		// aiff: the size of the FORM, of the sound chunk, and the number of frames in the common chunk
		{fixtures[8], func(d []byte) []int { return []int{4, indexOf(d, "SSND", 12) + 4, indexOf(d, "COMM", 12) + 10} }, binary.BigEndian},
		{fixtures[12], func(d []byte) []int { return []int{4, indexOf(d, "SSND", 12) + 4, indexOf(d, "COMM", 12) + 10} }, binary.BigEndian},
	}

	for _, tc := range cases {
		t.Run(tc.fix.file, func(t *testing.T) {
			path := patchSizes(t, tc.fix.file, tc.offsets, tc.order)

			sf, err := NewSoundFile[float32](path)
			if err != nil {
				t.Fatal(err)
			}

			ref := fixtureRef(t, tc.fix)
			if int(sf.NumFrames()) != len(ref[0]) {
				t.Fatalf("%d frames, want %d", sf.NumFrames(), len(ref[0]))
			}
			for c := range ref {
				for i, want := range ref[c] {
					if math.Abs(float64(sf.Buffer(c, 0)[i]-want)) > 1e-6 {
						t.Fatalf("channel %d frame %d differs", c, i)
					}
				}
			}

			r, err := Open[float32](path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = r.Close() }()

			if r.Info().Frames != int64(len(ref[0])) {
				t.Fatalf("the reader sees %d frames, want %d", r.Info().Frames, len(ref[0]))
			}
		})
	}
}
