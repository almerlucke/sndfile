package sndfile

import (
	"bytes"
	"encoding/binary"
	"io"
)

// An mp3 does not start and stop where the sound does. The first frame of a file of an encoder like LAME (and ffmpeg) holds no sound but a tag, Xing or
// Info, that says how long the sound is; the encoder starts its sound some samples late (the delay of the encoder), the decoder is some samples late
// itself, and the last frame is filled up (the padding). A decoder that gives all of it, as go-mp3 does, gives the sound some 1100 to 2300 frames late
// and some frames too long. For a sampler that is too much: a start or loop point in the file is not where it should be. mp3Gapless finds what is to be
// left out, as ffmpeg and libsndfile do.

// mp3DecoderDelay is the delay of the decoder (of the filter bank and the overlap of the layer 3 decoder) in frames, and one more, as ffmpeg has it
const mp3DecoderDelay = 528 + 1

// mp3Gapless is what is to be left out of what go-mp3 gives
type mp3Gapless struct {
	// skip is the number of frames at the start that are no sound: the frame with the tag, and the delays
	skip int64
	// valid is the number of frames of sound after those, -1 when the tag does not say
	valid int64
}

// noGapless is for a file without a tag: it is all sound
var noGapless = mp3Gapless{skip: 0, valid: -1}

// mp3Info finds the tag of the first frame of an mp3 file, and what it says about the delay and the length of the sound. A file that has no tag, or
// not one that this understands, is not changed: it gives noGapless
func mp3Info(r io.ReaderAt, size int64) mp3Gapless {
	// an ID3v2 tag may come first: 10 bytes and then its size (4 bytes of 7 bits), and a footer of 10 bytes if the flag is set
	var id3 [10]byte
	off := int64(0)
	if n, _ := r.ReadAt(id3[:], 0); n == 10 && bytes.Equal(id3[:3], []byte("ID3")) {
		off = 10 + int64(id3[6]&0x7F)<<21 + int64(id3[7]&0x7F)<<14 + int64(id3[8]&0x7F)<<7 + int64(id3[9]&0x7F)
		if id3[5]&0x10 != 0 {
			off += 10
		}
	}

	if off >= size {
		return noGapless
	}

	// the first frame is where the first valid header is, a little after the ID3v2 tag at most (padding, garbage)
	buf := make([]byte, 4096+512)
	n, _ := r.ReadAt(buf, off)
	buf = buf[:n]

	for i := 0; i+4 <= min(len(buf), 4096); i++ {
		if g, ok := parseMP3Frame(buf[i:]); ok {
			return g
		}
	}

	return noGapless
}

// parseMP3Frame reads the tag in a frame that starts at the beginning of b, if it is a frame of layer 3 and has one
func parseMP3Frame(b []byte) (mp3Gapless, bool) {
	// header: 11 bits of sync, 2 of version (3 is mpeg 1, 2 is mpeg 2, 0 is mpeg 2.5, 1 is no version), 2 of layer (1 is layer 3), 1 of protection (0 is
	// a crc after the header)
	if len(b) < 4 || b[0] != 0xFF || b[1]&0xE0 != 0xE0 {
		return noGapless, false
	}

	version, layer := int(b[1]>>3)&3, int(b[1]>>1)&3
	bitrate, rate := int(b[2]>>4), int(b[2]>>2)&3
	if version == 1 || layer != 1 || bitrate == 0xF || rate == 3 {
		return noGapless, false
	}

	mono := b[3]>>6 == 3

	samplesPerFrame, sideInfo := int64(1152), 32
	switch {
	case version != 3 && mono:
		samplesPerFrame, sideInfo = 576, 9
	case version != 3:
		samplesPerFrame, sideInfo = 576, 17
	case mono:
		sideInfo = 17
	}

	at := 4 + sideInfo
	if b[1]&1 == 0 {
		at += 2 // the crc
	}

	if len(b) < at+8 || (!bytes.Equal(b[at:at+4], []byte("Xing")) && !bytes.Equal(b[at:at+4], []byte("Info"))) {
		return noGapless, false
	}

	// the frame with the tag has no sound, and a decoder gives it as a frame of silence
	g := mp3Gapless{skip: samplesPerFrame, valid: -1}

	flags := binary.BigEndian.Uint32(b[at+4:])
	at += 8

	var frames int64 = -1
	if flags&1 != 0 {
		if len(b) < at+4 {
			return g, true
		}
		frames = int64(binary.BigEndian.Uint32(b[at:]))
		at += 4
	}
	if flags&2 != 0 { // bytes
		at += 4
	}
	if flags&4 != 0 { // table of contents
		at += 100
	}
	if flags&8 != 0 { // quality
		at += 4
	}

	// after it the tag of the encoder: its name (9 bytes), 12 bytes of settings, then the delay and the padding in 12 bits each
	if len(b) < at+24 {
		return g, true
	}

	name := string(b[at : at+4])
	if name != "LAME" && name != "Lavc" && name != "Lavf" {
		return g, true
	}

	d := b[at+21 : at+24]
	delay, padding := int64(d[0])<<4|int64(d[1])>>4, int64(d[1]&0x0F)<<8|int64(d[2])

	g.skip += delay + mp3DecoderDelay

	if frames >= 0 {
		if valid := frames*samplesPerFrame - delay - padding; valid >= 0 {
			g.valid = valid
		}
	}

	return g, true
}

// trim gives the part of total decoded frames that is sound, as the first frame and the number of frames
func (g mp3Gapless) trim(total int64) (start, n int64) {
	start = min(g.skip, total)
	n = total - start

	if g.valid >= 0 {
		n = min(n, g.valid)
	}

	return start, n
}
