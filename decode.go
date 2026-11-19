package sndfile

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/hajimehoshi/go-mp3"
	"github.com/jfreymuth/oggvorbis"
	"github.com/mewkiz/flac"

	"github.com/almerlucke/sndfile/float"
)

// decoded holds the result of a pure-Go decoder: deinterleaved channels
// normalized to [-1, 1].
type decoded[T float.Float] struct {
	channels   [][]T
	sampleRate int
}

// errNoDecoder signals that no pure-Go decoder recognizes the file, so the
// libsndfile fallback should be used.
var errNoDecoder = errors.New("sndfile: no pure-Go decoder for file")

// decodeFile sniffs the file header and decodes wav, aiff, mp3, flac and ogg vorbis. Encodings that are not handled give errNoDecoder.
func decodeFile[T float.Float](filePath string) (*decoded[T], error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	switch {
	case isWAV(data):
		return decodeWAV[T](data)
	case isAIFF(data):
		return decodeAIFF[T](data)
	case bytes.HasPrefix(data, []byte("fLaC")):
		return decodeFLAC[T](data)
	case bytes.HasPrefix(data, []byte("OggS")):
		return decodeOggVorbis[T](data)
	case isMP3(data):
		return decodeMP3[T](data)
	}

	return nil, errNoDecoder
}

// isMP3 detects an ID3v2 tag or an MPEG audio frame sync.
func isMP3(data []byte) bool {
	if bytes.HasPrefix(data, []byte("ID3")) {
		return true
	}

	return len(data) > 2 && data[0] == 0xFF && data[1]&0xE0 == 0xE0 && data[1]&0x18 != 0x08 && data[1]&0x06 != 0
}

// newChannels makes numChannels channels of frames samples that share one buffer
func newChannels[T float.Float](numChannels, frames int) [][]T {
	buf := make([]T, numChannels*frames)
	channels := make([][]T, numChannels)

	for c := range channels {
		channels[c] = buf[c*frames : (c+1)*frames : (c+1)*frames]
	}

	return channels
}

func decodeMP3[T float.Float](data []byte) (*decoded[T], error) {
	dec, err := mp3.NewDecoder(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	// go-mp3 always outputs interleaved stereo signed 16-bit little endian,
	// mono files are duplicated to both channels.
	raw, err := io.ReadAll(dec)
	if err != nil {
		return nil, err
	}

	// what is not sound is left out: the frame with the tag, the delays of the encoder and decoder, and the padding
	start, frames := mp3Info(bytes.NewReader(data), int64(len(data))).trim(int64(len(raw) / 4))
	raw = raw[start*4:]
	channels := newChannels[T](2, int(frames))

	for i := 0; i < int(frames); i++ {
		channels[0][i] = T(int16(binary.LittleEndian.Uint16(raw[4*i:]))) / 32768
		channels[1][i] = T(int16(binary.LittleEndian.Uint16(raw[4*i+2:]))) / 32768
	}

	return &decoded[T]{channels: channels, sampleRate: dec.SampleRate()}, nil
}

func decodeOggVorbis[T float.Float](data []byte) (*decoded[T], error) {
	samples, format, err := oggvorbis.ReadAll(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	if format.Channels < 1 {
		return nil, fmt.Errorf("sndfile: invalid ogg channel count %d", format.Channels)
	}

	frames := len(samples) / format.Channels
	channels := newChannels[T](format.Channels, frames)

	for i := 0; i < frames; i++ {
		for c := range channels {
			channels[c][i] = T(samples[i*format.Channels+c])
		}
	}

	return &decoded[T]{channels: channels, sampleRate: format.SampleRate}, nil
}

func decodeFLAC[T float.Float](data []byte) (*decoded[T], error) {
	stream, err := flac.New(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()

	numChannels := int(stream.Info.NChannels)
	scale := 1 / T(int64(1)<<(stream.Info.BitsPerSample-1))
	channels := make([][]T, numChannels)

	for c := range channels {
		channels[c] = make([]T, 0, stream.Info.NSamples)
	}

	for {
		frame, err := stream.ParseNext()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		for c := range channels {
			for _, s := range frame.Subframes[c].Samples {
				channels[c] = append(channels[c], T(s)*scale)
			}
		}
	}

	return &decoded[T]{channels: channels, sampleRate: int(stream.Info.SampleRate)}, nil
}
