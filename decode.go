package sndfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/hajimehoshi/go-mp3"
	"github.com/jfreymuth/oggvorbis"
	"github.com/mewkiz/flac"
)

// decoded holds the result of a pure-Go decoder: deinterleaved channels
// normalized to [-1, 1].
type decoded struct {
	channels   [][]float32
	sampleRate int
}

// errNoDecoder signals that no pure-Go decoder recognizes the file, so the
// libsndfile fallback should be used.
var errNoDecoder = errors.New("sndfile: no pure-Go decoder for file")

// decodeFile sniffs the file header and decodes mp3, flac and ogg vorbis.
func decodeFile(filePath string) (*decoded, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	switch {
	case bytes.HasPrefix(data, []byte("fLaC")):
		return decodeFLAC(data)
	case bytes.HasPrefix(data, []byte("OggS")):
		return decodeOggVorbis(data)
	case isMP3(data):
		return decodeMP3(data)
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

func deinterleave(samples []float32, numChannels int) [][]float32 {
	frames := len(samples) / numChannels
	buf := make([]float32, frames*numChannels)
	channels := make([][]float32, numChannels)

	for c := range channels {
		channels[c] = buf[c*frames : (c+1)*frames]
	}

	for i := 0; i < frames; i++ {
		for c := 0; c < numChannels; c++ {
			channels[c][i] = samples[i*numChannels+c]
		}
	}

	return channels
}

func decodeMP3(data []byte) (*decoded, error) {
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

	samples := make([]float32, len(raw)/2)
	for i := range samples {
		samples[i] = float32(int16(uint16(raw[2*i])|uint16(raw[2*i+1])<<8)) / 32768
	}

	return &decoded{channels: deinterleave(samples, 2), sampleRate: dec.SampleRate()}, nil
}

func decodeOggVorbis(data []byte) (*decoded, error) {
	samples, format, err := oggvorbis.ReadAll(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	if format.Channels < 1 {
		return nil, fmt.Errorf("sndfile: invalid ogg channel count %d", format.Channels)
	}

	return &decoded{channels: deinterleave(samples, format.Channels), sampleRate: format.SampleRate}, nil
}

func decodeFLAC(data []byte) (*decoded, error) {
	stream, err := flac.New(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()

	numChannels := int(stream.Info.NChannels)
	scale := 1 / float32(int64(1)<<(stream.Info.BitsPerSample-1))
	channels := make([][]float32, numChannels)

	for c := range channels {
		channels[c] = make([]float32, 0, stream.Info.NSamples)
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
				channels[c] = append(channels[c], float32(s)*scale)
			}
		}
	}

	return &decoded{channels: channels, sampleRate: int(stream.Info.SampleRate)}, nil
}
