package sndfile

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"os"
	"testing"
)

// mp3TagFrame makes the first frame of an mp3 file with an Info tag and the tag of an encoder: version 3 is mpeg 1, 2 is mpeg 2 and 0 is mpeg 2.5
func mp3TagFrame(version int, mono, crc bool, encoder string, frames, delay, padding int) []byte {
	prot := byte(1)
	if crc {
		prot = 0
	}
	mode := byte(0)
	if mono {
		mode = 3
	}

	b := []byte{0xFF, 0xE0 | byte(version)<<3 | 1<<1 | prot, 9 << 4, mode << 6}
	if crc {
		b = append(b, 0, 0)
	}

	side := 32
	switch {
	case version != 3 && mono:
		side = 9
	case version != 3:
		side = 17
	case mono:
		side = 17
	}
	b = append(b, make([]byte, side)...)

	b = append(b, "Info"...)
	flags := uint32(0)
	if frames >= 0 {
		flags = 1
	}
	b = binary.BigEndian.AppendUint32(b, flags)
	if frames >= 0 {
		b = binary.BigEndian.AppendUint32(b, uint32(frames))
	}

	// the name of the encoder (9 bytes), 12 bytes of settings, and the delay and padding
	name := make([]byte, 9)
	copy(name, encoder)
	b = append(b, name...)
	b = append(b, make([]byte, 12)...)
	b = append(b, byte(delay>>4), byte(delay&0xF)<<4|byte(padding>>8), byte(padding))

	return append(b, make([]byte, 200)...)
}

func TestMP3GaplessTag(t *testing.T) {
	for _, version := range []int{3, 2, 0} {
		for _, mono := range []bool{false, true} {
			for _, crc := range []bool{false, true} {
				spf := int64(576)
				if version == 3 {
					spf = 1152
				}

				const frames, delay, padding = 40, 576, 1100
				data := mp3TagFrame(version, mono, crc, "LAME3.99r", frames, delay, padding)

				got := mp3Info(bytes.NewReader(data), int64(len(data)))
				want := mp3Gapless{skip: spf + delay + mp3DecoderDelay, valid: frames*spf - delay - padding}
				if got != want {
					t.Errorf("version %d mono %v crc %v: %+v, want %+v", version, mono, crc, got, want)
				}
			}
		}
	}
}

func TestMP3GaplessVariants(t *testing.T) {
	info := func(data []byte) mp3Gapless { return mp3Info(bytes.NewReader(data), int64(len(data))) }

	t.Run("encoder names", func(t *testing.T) {
		for _, name := range []string{"LAME3.100", "Lavc58.13", "Lavf58.76"} {
			if g := info(mp3TagFrame(3, false, false, name, 10, 576, 900)); g.skip != 1152+576+mp3DecoderDelay {
				t.Errorf("%s: %+v", name, g)
			}
		}
	})

	t.Run("an encoder that is not known only loses the frame of the tag", func(t *testing.T) {
		if g := info(mp3TagFrame(3, false, false, "XING Encoder", 10, 576, 900)); g != (mp3Gapless{skip: 1152, valid: -1}) {
			t.Errorf("%+v", g)
		}
	})

	t.Run("no number of frames: the start is cut and the end is not", func(t *testing.T) {
		if g := info(mp3TagFrame(2, false, false, "LAME3.99r", -1, 576, 900)); g != (mp3Gapless{skip: 576 + 576 + mp3DecoderDelay, valid: -1}) {
			t.Errorf("%+v", g)
		}
	})

	t.Run("no tag at all", func(t *testing.T) {
		data := mp3TagFrame(3, false, false, "LAME3.99r", 10, 576, 900)
		copy(data[36:40], "XXXX") // where Info is
		if g := info(data); g != noGapless {
			t.Errorf("%+v", g)
		}
		if g := info(nil); g != noGapless {
			t.Errorf("nil: %+v", g)
		}
	})

	t.Run("a tag after an ID3v2 tag", func(t *testing.T) {
		const id3Size = 6000 // more than the window in which a frame is looked for
		id3 := append([]byte("ID3\x04\x00\x00"), byte(id3Size>>21&0x7F), byte(id3Size>>14&0x7F), byte(id3Size>>7&0x7F), byte(id3Size&0x7F))
		data := append(append(id3, make([]byte, id3Size)...), mp3TagFrame(3, false, false, "LAME3.99r", 10, 576, 900)...)

		if g := info(data); g.skip != 1152+576+mp3DecoderDelay || g.valid != 10*1152-576-900 {
			t.Errorf("%+v", g)
		}
	})

	t.Run("a delay that is more than the frames is no negative sound", func(t *testing.T) {
		g := info(mp3TagFrame(3, false, false, "LAME3.99r", 1, 1000, 1000))
		if g.valid != -1 {
			t.Errorf("%+v", g)
		}
		if start, n := g.trim(1152 * 2); start < 0 || n < 0 {
			t.Errorf("trim gave %d, %d", start, n)
		}
	})
}

func TestMP3GaplessTrim(t *testing.T) {
	for _, tc := range []struct {
		g          mp3Gapless
		total      int64
		start, len int64
	}{
		{mp3Gapless{skip: 1000, valid: 500}, 5000, 1000, 500},
		{mp3Gapless{skip: 1000, valid: -1}, 5000, 1000, 4000},
		{mp3Gapless{skip: 1000, valid: 9000}, 5000, 1000, 4000}, // the tag claims more than there is
		{mp3Gapless{skip: 9000, valid: 500}, 5000, 5000, 0},     // all of it is delay
		{noGapless, 5000, 0, 5000},
		{noGapless, 0, 0, 0},
	} {
		if start, n := tc.g.trim(tc.total); start != tc.start || n != tc.len {
			t.Errorf("%+v of %d: %d, %d, want %d, %d", tc.g, tc.total, start, n, tc.start, tc.len)
		}
	}
}

// TestMP3GaplessMalformed: bytes that are no tag, or half of one, are no reason for a panic
func TestMP3GaplessMalformed(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	good := mp3TagFrame(3, false, true, "LAME3.99r", 10, 576, 900)

	for n := 0; n <= len(good); n++ {
		mp3Info(bytes.NewReader(good[:n]), int64(n))
	}

	for range 2000 {
		data := append([]byte(nil), good...)
		for range 1 + rng.Intn(6) {
			data[rng.Intn(len(data))] = byte(rng.Intn(256))
		}
		g := mp3Info(bytes.NewReader(data), int64(len(data)))
		if start, n := g.trim(10000); start < 0 || n < 0 || start+n > 10000 {
			t.Fatalf("%+v trims to %d, %d", g, start, n)
		}
	}

	for range 500 {
		data := make([]byte, 100+rng.Intn(5000))
		rng.Read(data)
		mp3Info(bytes.NewReader(data), int64(len(data)))
	}
}

// the fixture again, with the first frame (the tag) taken away, or with another ID3v2 tag in front of it: the sound is the same, or is not cut at all
func TestMP3GaplessFixtureVariants(t *testing.T) {
	orig, err := os.ReadFile(fixturePath("mp3_22k_st.mp3"))
	if err != nil {
		t.Fatal(err)
	}

	// the fixture has an ID3v2 tag of its own: take it off
	id3Len := 10 + int(orig[6])<<21 + int(orig[7])<<14 + int(orig[8])<<7 + int(orig[9])
	audio := orig[id3Len:]

	write := func(data []byte) string {
		p := t.TempDir() + "/x.mp3"
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	want, err := NewSoundFile[float32](fixturePath("mp3_22k_st.mp3"))
	if err != nil {
		t.Fatal(err)
	}

	t.Run("a bigger ID3v2 tag", func(t *testing.T) {
		const size = 20000
		id3 := append([]byte("ID3\x04\x00\x00"), byte(size>>21&0x7F), byte(size>>14&0x7F), byte(size>>7&0x7F), byte(size&0x7F))
		path := write(append(append(id3, make([]byte, size)...), audio...))

		got, err := NewSoundFile[float32](path)
		if err != nil {
			t.Fatal(err)
		}
		if got.NumFrames() != want.NumFrames() {
			t.Fatalf("%d frames, want %d", got.NumFrames(), want.NumFrames())
		}
		for c := 0; c < 2; c++ {
			for i := range want.Buffer(c, 0) {
				if got.Buffer(c, 0)[i] != want.Buffer(c, 0)[i] {
					t.Fatalf("channel %d frame %d differs", c, i)
				}
			}
		}

		r, err := Open[float32](path)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.Close() }()
		if r.Info().Frames != want.NumFrames() {
			t.Fatalf("the reader has %d frames, want %d", r.Info().Frames, want.NumFrames())
		}
	})

	t.Run("without the tag nothing is cut", func(t *testing.T) {
		// the second frame starts where the next valid header is: the first has the tag and is nearly all zeros
		next := -1
		for i := 4; i+4 <= len(audio); i++ {
			if audio[i] == 0xFF && audio[i+1] == audio[1] && audio[i+2]>>4 != 0xF && audio[i+2]>>2&3 != 3 {
				next = i
				break
			}
		}
		if next < 0 {
			t.Fatal("no second frame")
		}

		got, err := NewSoundFile[float32](write(audio[next:]))
		if err != nil {
			t.Fatal(err)
		}

		// 10 frames of 576 frames, all of them sound
		if got.NumFrames() != 10*576 {
			t.Fatalf("%d frames, want %d", got.NumFrames(), 10*576)
		}
	})
}
