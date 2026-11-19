package wav_test

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/almerlucke/sndfile"
	"github.com/almerlucke/sndfile/writer/backend/wav"
)

// TestHeaderIsRefreshed: a file that is not closed (the program stopped) has the samples of the last refresh, and not none
func TestHeaderIsRefreshed(t *testing.T) {
	defer wav.SetRefreshBytes(4096)()

	path := filepath.Join(t.TempDir(), "crash.wav")
	w, err := wav.New(path, 2, 8000)
	if err != nil {
		t.Fatal(err)
	}

	// a block is 500 frames, 4000 bytes: the header is refreshed after every second one
	block := func(n int) []float32 {
		b := make([]float32, 1000)
		for i := range b {
			b[i] = float32(math.Sin(float64(n*1000+i) * 0.01))
		}
		return b
	}

	const blocks = 51
	for n := 0; n < blocks; n++ {
		if err := w.Write(block(n)); err != nil {
			t.Fatal(err)
		}
	}

	// no Close: this is what the disk has when the program stops here. The sizes in the header are those of the last refresh: other programs than ours
	// trust them (a size of 0 is an empty file for them)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := binary.LittleEndian.Uint32(raw[54:]), uint32(50*500*2*4); got != want {
		t.Fatalf("data size in the header of the unclosed file is %d, want %d", got, want)
	}
	if got, want := binary.LittleEndian.Uint32(raw[4:]), uint32(50+50*500*2*4); got != want {
		t.Fatalf("RIFF size in the header of the unclosed file is %d, want %d", got, want)
	}

	sf, err := sndfile.NewSoundFile[float32](path)
	if err != nil {
		t.Fatal(err)
	}

	if want := int64(50 * 500); sf.NumFrames() != want {
		t.Fatalf("%d frames of the unclosed file, want the %d of the last refresh", sf.NumFrames(), want)
	}
	for n := 0; n < 50; n++ {
		b := block(n)
		for i := 0; i < 500; i++ {
			if sf.Buffer(0, 0)[n*500+i] != b[2*i] || sf.Buffer(1, 0)[n*500+i] != b[2*i+1] {
				t.Fatalf("block %d frame %d differs", n, i)
			}
		}
	}

	// the refresh left the position at the end: what is written after it is where it should be
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	sf, err = sndfile.NewSoundFile[float32](path)
	if err != nil {
		t.Fatal(err)
	}
	if want := int64(blocks * 500); sf.NumFrames() != want {
		t.Fatalf("%d frames of the closed file, want %d", sf.NumFrames(), want)
	}
	last := block(blocks - 1)
	if got := sf.Buffer(0, 0)[(blocks-1)*500]; got != last[0] {
		t.Fatalf("the block written after the last refresh is %v, want %v", got, last[0])
	}
}
