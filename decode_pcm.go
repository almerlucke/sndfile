package sndfile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
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

// maxHeaderChunk is the largest fmt or COMM chunk that is read: they are small, a larger one is not a header
const maxHeaderChunk = 1 << 16

// chunkWalker goes over the chunks of a RIFF or IFF container in a file, reading the headers only: the samples are not read
type chunkWalker struct {
	r     io.ReaderAt
	size  int64
	off   int64 // where the next chunk header is
	order binary.ByteOrder
}

// chunk is a chunk found by the walker
type chunk struct {
	id string
	// at is where the body of the chunk starts and n how long it is. A chunk that claims more than is left, or a data chunk with a placeholder size
	// (0 or 0xFFFFFFFF, from a writer that did not finish), runs to the end of the file: streamed and unfinished files still have all their samples
	at, n int64
}

func (w *chunkWalker) next() (chunk, bool) {
	var h [8]byte
	if w.off < 0 || w.off+8 > w.size {
		return chunk{}, false
	}
	if _, err := w.r.ReadAt(h[:], w.off); err != nil {
		return chunk{}, false
	}

	c := chunk{id: string(h[0:4]), at: w.off + 8}
	declared := w.order.Uint32(h[4:8])
	left := w.size - c.at
	n := int64(declared)

	if c.id == "data" && (declared == 0 || declared == math.MaxUint32) && left > 0 {
		n = left
	}

	c.n = min(n, left)
	w.off = c.at + c.n

	if c.n%2 == 1 && w.off < w.size {
		w.off++ // chunks are padded to an even size
	}

	return c, true
}

// body reads the body of a (small) chunk
func (w *chunkWalker) body(c chunk) ([]byte, error) {
	if c.n > maxHeaderChunk {
		return nil, errTruncated
	}

	b := make([]byte, c.n)
	if _, err := w.r.ReadAt(b, c.at); err != nil && !(err == io.EOF && int64(len(b)) == c.n) {
		return nil, errTruncated
	}

	return b, nil
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

func (l pcmLayout) frameSize() int { return l.numChannels * l.bytesPer }

// pcmFile is where the samples of a wav or aiff file are: layout, and the offset and length in bytes of the sample data
type pcmFile struct {
	layout       pcmLayout
	dataOff, len int64
}

// frames is the number of whole frames
func (f pcmFile) frames() int64 { return f.len / int64(f.layout.frameSize()) }

// convertChannel converts the samples of one channel of interleaved sample bytes into dst, at every stride-th place. A final partial frame is not read
func convertChannel[T float.Float](dst []T, stride int, src []byte, l pcmLayout, channel, frames int) {
	frameSize := l.frameSize()
	var order binary.ByteOrder = binary.LittleEndian
	if l.bigEndian {
		order = binary.BigEndian
	}

	// signed integers are scaled to [-1, 1) by the size of their bit depth
	scale := 1 / T(uint64(1)<<(8*l.bytesPer-1))
	at := channel * l.bytesPer

	switch {
	case l.float && l.bytesPer == 4:
		for i := 0; i < frames; i++ {
			dst[i*stride] = T(math.Float32frombits(order.Uint32(src[i*frameSize+at:])))
		}
	case l.float:
		for i := 0; i < frames; i++ {
			dst[i*stride] = T(math.Float64frombits(order.Uint64(src[i*frameSize+at:])))
		}
	case l.unsigned8:
		for i := 0; i < frames; i++ {
			dst[i*stride] = (T(src[i*frameSize+at]) - 128) * scale
		}
	case l.bytesPer == 1:
		for i := 0; i < frames; i++ {
			dst[i*stride] = T(int8(src[i*frameSize+at])) * scale
		}
	case l.bytesPer == 2:
		for i := 0; i < frames; i++ {
			dst[i*stride] = T(int16(order.Uint16(src[i*frameSize+at:]))) * scale
		}
	case l.bytesPer == 3:
		for i := 0; i < frames; i++ {
			b := src[i*frameSize+at:]
			var x int32
			if l.bigEndian {
				x = int32(b[0])<<16 | int32(b[1])<<8 | int32(b[2])
			} else {
				x = int32(b[2])<<16 | int32(b[1])<<8 | int32(b[0])
			}
			dst[i*stride] = T(x<<8>>8) * scale // sign extend 24 bits
		}
	default:
		for i := 0; i < frames; i++ {
			dst[i*stride] = T(int32(order.Uint32(src[i*frameSize+at:]))) * scale
		}
	}
}

// readPCM converts all the interleaved sample bytes into channels
func readPCM[T float.Float](data []byte, l pcmLayout) *decoded[T] {
	frames := len(data) / l.frameSize()
	channels := newChannels[T](l.numChannels, frames)

	for c := range channels {
		convertChannel(channels[c], 1, data, l, c, frames)
	}

	return &decoded[T]{channels: channels, sampleRate: l.sampleRate}
}

const (
	wavFormatPCM        = 1
	wavFormatFloat      = 3
	wavFormatExtensible = 0xFFFE
)

// parseWAV finds the layout and the samples of a wav file with PCM (8, 16, 24 and 32 bit) or float (32 and 64 bit) samples, also with the extensible
// header. Other encodings (adpcm, a-law, rf64, ...) give errNoDecoder so that libsndfile gets them
func parseWAV(r io.ReaderAt, size int64) (pcmFile, error) {
	var f pcmFile
	haveFmt, haveData := false, false
	w := &chunkWalker{r: r, size: size, off: 12, order: binary.LittleEndian}

	for !(haveFmt && haveData) {
		c, ok := w.next()
		if !ok {
			break
		}

		switch c.id {
		case "fmt ":
			b, err := w.body(c)
			if err != nil || len(b) < 16 {
				return f, errTruncated
			}

			format := int(binary.LittleEndian.Uint16(b[0:]))
			f.layout.numChannels = int(binary.LittleEndian.Uint16(b[2:]))
			f.layout.sampleRate = int(binary.LittleEndian.Uint32(b[4:]))
			bits := int(binary.LittleEndian.Uint16(b[14:]))

			if format == wavFormatExtensible && len(b) >= 26 {
				// the first two bytes of the sub format guid are the format
				format = int(binary.LittleEndian.Uint16(b[24:]))
			}

			switch {
			case format == wavFormatPCM && (bits == 8 || bits == 16 || bits == 24 || bits == 32):
				f.layout.unsigned8 = bits == 8
			case format == wavFormatFloat && (bits == 32 || bits == 64):
				f.layout.float = true
			default:
				return f, errNoDecoder
			}

			f.layout.bytesPer = bits / 8
			haveFmt = true
		case "data":
			f.dataOff, f.len = c.at, c.n
			haveData = true
		}
	}

	if !haveFmt || !haveData || f.layout.numChannels < 1 || f.layout.sampleRate < 1 {
		return f, errTruncated
	}

	return f, nil
}

// parseAIFF finds the layout and the samples of an aiff or aifc file with PCM (8 to 32 bit, big endian or little endian 'sowt') or float ('fl32',
// 'fl64') samples. Other encodings give errNoDecoder
func parseAIFF(r io.ReaderAt, size int64) (pcmFile, error) {
	var f pcmFile
	var head [4]byte
	if _, err := r.ReadAt(head[:], 8); err != nil {
		return f, errTruncated
	}
	isAIFC := bytes.Equal(head[:], []byte("AIFC"))

	var frames uint32
	haveComm, haveSound := false, false
	f.layout.bigEndian = true
	w := &chunkWalker{r: r, size: size, off: 12, order: binary.BigEndian}

	for !(haveComm && haveSound) {
		c, ok := w.next()
		if !ok {
			break
		}

		switch c.id {
		case "COMM":
			b, err := w.body(c)
			minSize := 18
			if isAIFC {
				minSize = 22
			}
			if err != nil || len(b) < minSize {
				return f, errTruncated
			}

			f.layout.numChannels = int(binary.BigEndian.Uint16(b[0:]))
			frames = binary.BigEndian.Uint32(b[2:])
			bits := int(binary.BigEndian.Uint16(b[6:]))
			f.layout.sampleRate = int(math.Round(float80.NewFromBits(binary.BigEndian.Uint16(b[8:]), binary.BigEndian.Uint64(b[10:])).Float64()))

			compression := "NONE"
			if isAIFC {
				compression = string(b[18:22])
			}

			switch compression {
			case "NONE", "twos":
				if bits < 1 || bits > 32 {
					return f, errNoDecoder
				}
				f.layout.bytesPer = (bits + 7) / 8
			case "sowt":
				if bits < 1 || bits > 32 {
					return f, errNoDecoder
				}
				f.layout.bytesPer = (bits + 7) / 8
				f.layout.bigEndian = false
			case "fl32", "FL32":
				f.layout.bytesPer, f.layout.float = 4, true
			case "fl64", "FL64":
				f.layout.bytesPer, f.layout.float = 8, true
			default:
				return f, errNoDecoder
			}

			haveComm = true
		case "SSND":
			var h [8]byte
			if c.n < 8 {
				return f, errTruncated
			}
			if _, err := r.ReadAt(h[:], c.at); err != nil {
				return f, errTruncated
			}

			offset := int64(binary.BigEndian.Uint32(h[0:]))
			if 8+offset > c.n {
				return f, errTruncated
			}

			f.dataOff, f.len = c.at+8+offset, c.n-8-offset
			haveSound = true
		}
	}

	if !haveComm || !haveSound || f.layout.numChannels < 1 || f.layout.sampleRate < 1 {
		return f, errTruncated
	}

	// the number of frames in the header is leading when the data is longer (padding)
	f.len = min(f.len, int64(frames)*int64(f.layout.frameSize()))

	return f, nil
}

// decodeWAV reads a whole wav file that is in memory
func decodeWAV[T float.Float](data []byte) (*decoded[T], error) {
	f, err := parseWAV(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}

	return readPCM[T](data[f.dataOff:f.dataOff+f.len], f.layout), nil
}

// decodeAIFF reads a whole aiff or aifc file that is in memory
func decodeAIFF[T float.Float](data []byte) (*decoded[T], error) {
	f, err := parseAIFF(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}

	return readPCM[T](data[f.dataOff:f.dataOff+f.len], f.layout), nil
}
