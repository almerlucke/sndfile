package sndfile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"

	"github.com/almerlucke/sndfile/float"
	"github.com/almerlucke/sndfile/writer/backend/aifc/float80"
)

func isWAV(data []byte) bool {
	return len(data) >= 12 && bytes.Equal(data[0:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WAVE"))
}

func isAIFF(data []byte) bool {
	return len(data) >= 12 && bytes.Equal(data[0:4], []byte("FORM")) &&
		(bytes.Equal(data[8:12], []byte("AIFF")) || bytes.Equal(data[8:12], []byte("AIFC")))
}

var errTruncated = errors.New("sndfile: truncated or malformed file")

// chunk is one chunk of a RIFF or IFF container
type chunk struct {
	id   string
	data []byte
	// declared is the size in the header, which can be a placeholder (0 or 0xFFFFFFFF) when a writer did not finish
	declared uint32
}

// nextChunk reads the chunk header at the start of buf and gives the chunk and the rest after it (and its pad byte). A chunk that claims more than
// is left, or is a placeholder size, runs to the end of the file: streamed and unfinished files are common and still have all their samples
func nextChunk(buf []byte, order binary.ByteOrder) (chunk, []byte, bool) {
	if len(buf) < 8 {
		return chunk{}, nil, false
	}

	c := chunk{id: string(buf[0:4]), declared: order.Uint32(buf[4:8])}
	body := buf[8:]
	size := uint64(c.declared)

	if c.id == "data" && (c.declared == 0 || c.declared == math.MaxUint32) && len(body) > 0 {
		size = uint64(len(body))
	}

	if size > uint64(len(body)) {
		size = uint64(len(body))
	}

	c.data = body[:size]
	rest := body[size:]

	if size%2 == 1 && len(rest) > 0 {
		rest = rest[1:]
	}

	return c, rest, true
}

// pcmLayout describes how the samples of a file are stored
type pcmLayout struct {
	numChannels int
	sampleRate  int
	bytesPer    int  // bytes per sample
	bigEndian   bool // sample byte order
	float       bool
	unsigned8   bool // wav 8 bit is unsigned
}

// readPCM converts interleaved sample bytes into channels. A final partial frame is dropped
func readPCM[T float.Float](data []byte, l pcmLayout) *decoded[T] {
	frameSize := l.numChannels * l.bytesPer
	frames := len(data) / frameSize
	channels := newChannels[T](l.numChannels, frames)

	var order binary.ByteOrder = binary.LittleEndian
	if l.bigEndian {
		order = binary.BigEndian
	}

	// signed integers are scaled to [-1, 1) by the size of their bit depth
	scale := 1 / T(uint64(1)<<(8*l.bytesPer-1))

	for i := 0; i < frames; i++ {
		for c := 0; c < l.numChannels; c++ {
			b := data[i*frameSize+c*l.bytesPer:]
			var v T

			switch {
			case l.float && l.bytesPer == 4:
				v = T(math.Float32frombits(order.Uint32(b)))
			case l.float:
				v = T(math.Float64frombits(order.Uint64(b)))
			case l.unsigned8:
				v = (T(b[0]) - 128) * scale
			case l.bytesPer == 1:
				v = T(int8(b[0])) * scale
			case l.bytesPer == 2:
				v = T(int16(order.Uint16(b))) * scale
			case l.bytesPer == 3:
				var x int32
				if l.bigEndian {
					x = int32(b[0])<<16 | int32(b[1])<<8 | int32(b[2])
				} else {
					x = int32(b[2])<<16 | int32(b[1])<<8 | int32(b[0])
				}
				x = x << 8 >> 8 // sign extend 24 bits
				v = T(x) * scale
			default:
				v = T(int32(order.Uint32(b))) * scale
			}

			channels[c][i] = v
		}
	}

	return &decoded[T]{channels: channels, sampleRate: l.sampleRate}
}

const (
	wavFormatPCM        = 1
	wavFormatFloat      = 3
	wavFormatExtensible = 0xFFFE
)

// decodeWAV reads PCM (8, 16, 24 and 32 bit) and float (32 and 64 bit) wav files, also with the extensible header. Other encodings (adpcm, a-law,
// rf64, ...) give errNoDecoder so that libsndfile gets them
func decodeWAV[T float.Float](data []byte) (*decoded[T], error) {
	var layout pcmLayout
	var samples []byte
	haveFmt, haveData := false, false

	rest := data[12:]
	for {
		c, next, ok := nextChunk(rest, binary.LittleEndian)
		if !ok {
			break
		}
		rest = next

		switch c.id {
		case "fmt ":
			if len(c.data) < 16 {
				return nil, errTruncated
			}

			format := int(binary.LittleEndian.Uint16(c.data[0:]))
			layout.numChannels = int(binary.LittleEndian.Uint16(c.data[2:]))
			layout.sampleRate = int(binary.LittleEndian.Uint32(c.data[4:]))
			bits := int(binary.LittleEndian.Uint16(c.data[14:]))

			if format == wavFormatExtensible && len(c.data) >= 26 {
				// the first two bytes of the sub format guid are the format
				format = int(binary.LittleEndian.Uint16(c.data[24:]))
			}

			switch {
			case format == wavFormatPCM && (bits == 8 || bits == 16 || bits == 24 || bits == 32):
				layout.unsigned8 = bits == 8
			case format == wavFormatFloat && (bits == 32 || bits == 64):
				layout.float = true
			default:
				return nil, errNoDecoder
			}

			layout.bytesPer = bits / 8
			haveFmt = true
		case "data":
			samples = c.data
			haveData = true
		}

		if haveFmt && haveData {
			break
		}
	}

	if !haveFmt || !haveData {
		return nil, errTruncated
	}

	if layout.numChannels < 1 || layout.sampleRate < 1 {
		return nil, errTruncated
	}

	return readPCM[T](samples, layout), nil
}

// decodeAIFF reads aiff and aifc files with PCM (8 to 32 bit, big endian or little endian 'sowt') and float ('fl32', 'fl64') samples
func decodeAIFF[T float.Float](data []byte) (*decoded[T], error) {
	isAIFC := bytes.Equal(data[8:12], []byte("AIFC"))

	var layout pcmLayout
	var samples []byte
	var frames uint32
	haveComm, haveSound := false, false
	layout.bigEndian = true

	rest := data[12:]
	for {
		c, next, ok := nextChunk(rest, binary.BigEndian)
		if !ok {
			break
		}
		rest = next

		switch c.id {
		case "COMM":
			minSize := 18
			if isAIFC {
				minSize = 22
			}
			if len(c.data) < minSize {
				return nil, errTruncated
			}

			layout.numChannels = int(binary.BigEndian.Uint16(c.data[0:]))
			frames = binary.BigEndian.Uint32(c.data[2:])
			bits := int(binary.BigEndian.Uint16(c.data[6:]))
			layout.sampleRate = int(math.Round(float80.NewFromBits(binary.BigEndian.Uint16(c.data[8:]), binary.BigEndian.Uint64(c.data[10:])).Float64()))

			compression := "NONE"
			if isAIFC {
				compression = string(c.data[18:22])
			}

			switch compression {
			case "NONE", "twos":
				if bits < 1 || bits > 32 {
					return nil, errNoDecoder
				}
				layout.bytesPer = (bits + 7) / 8
			case "sowt":
				if bits < 1 || bits > 32 {
					return nil, errNoDecoder
				}
				layout.bytesPer = (bits + 7) / 8
				layout.bigEndian = false
			case "fl32", "FL32":
				layout.bytesPer, layout.float = 4, true
			case "fl64", "FL64":
				layout.bytesPer, layout.float = 8, true
			default:
				return nil, errNoDecoder
			}

			haveComm = true
		case "SSND":
			if len(c.data) < 8 {
				return nil, errTruncated
			}

			offset := uint64(binary.BigEndian.Uint32(c.data[0:]))
			if 8+offset > uint64(len(c.data)) {
				return nil, errTruncated
			}

			samples = c.data[8+offset:]
			haveSound = true
		}

		if haveComm && haveSound {
			break
		}
	}

	if !haveComm || !haveSound {
		return nil, errTruncated
	}

	if layout.numChannels < 1 || layout.sampleRate < 1 {
		return nil, errTruncated
	}

	// the number of frames in the header is leading when the data is longer (padding)
	if want := uint64(frames) * uint64(layout.numChannels) * uint64(layout.bytesPer); want < uint64(len(samples)) {
		samples = samples[:want]
	}

	return readPCM[T](samples, layout), nil
}
