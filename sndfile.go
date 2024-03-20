package sndfile

import (
	"github.com/mkb218/gosndfile/sndfile"
)

type SoundFiler interface {
	NumChannels() int
	SampleRate() float64
	NumFrames() int64
	Duration() float64
	Depth() int
	Buffer(channel int, depth int) []float64
	Lookup(pos float64, channel int, depth int, wrap bool) float64
	LookupAll(pos float64, depth int, wrap bool) []float64
}

// SoundFile contains sound file deinterleaved samples and implements SoundFiler interface
type SoundFile struct {
	// Deinterleaved channels
	channels [][]float64
	// Sample rate
	sampleRate float64
	// Number of frames
	numFrames int64
	// Duration in seconds
	duration float64
	// Lookup output
	out []float64
}

// NewSoundFile load sound file from disk
func NewSoundFile(filePath string) (*SoundFile, error) {
	info := sndfile.Info{}

	file, err := sndfile.Open(filePath, sndfile.Read, &info)
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = file.Close()
	}()

	// Create one big buffer to hold all samples
	fileBuffer := make([]float64, int64(info.Channels)*info.Frames)

	// Create separate channels by splitting buffer into info.Channels parts
	channels := make([][]float64, info.Channels)
	for i := int32(0); i < info.Channels; i++ {
		channels[i] = fileBuffer[int64(i)*info.Frames : int64(i+1)*info.Frames]
	}

	// Deinterleave in blocks
	sampleBlockSize := int64(2048) * int64(info.Channels)
	samples := make([]float64, sampleBlockSize)
	frameIndex := int64(0)

	for {
		framesRead, err := file.ReadFrames(samples)
		if err != nil {
			return nil, err
		}

		if framesRead == 0 {
			break
		}

		for i := int64(0); i < framesRead; i++ {
			for j := int64(0); j < int64(info.Channels); j++ {
				channels[j][frameIndex+i] = samples[i*int64(info.Channels)+j]
			}
		}

		frameIndex += framesRead
	}

	sf := SoundFile{}
	sf.duration = float64(info.Frames) / float64(info.Samplerate)
	sf.numFrames = info.Frames
	sf.channels = channels
	sf.sampleRate = float64(info.Samplerate)
	sf.out = make([]float64, info.Channels)

	return &sf, nil
}

func (sf *SoundFile) NumChannels() int {
	return len(sf.channels)
}

func (sf *SoundFile) SampleRate() float64 {
	return sf.sampleRate
}

func (sf *SoundFile) NumFrames() int64 {
	return sf.numFrames
}

func (sf *SoundFile) Duration() float64 {
	return sf.duration
}

func (sf *SoundFile) Depth() int {
	return 1
}

func (sf *SoundFile) Buffer(channel int, _ int) []float64 {
	return sf.channels[channel]
}

func (sf *SoundFile) Lookup(pos float64, channel int, _ int, wrap bool) float64 {
	lp := NewLookupParam(pos, sf.numFrames, wrap)
	return lp.Lookup(sf.channels[channel])
}

func (sf *SoundFile) LookupAll(pos float64, _ int, wrap bool) []float64 {
	lp := NewLookupParam(pos, sf.numFrames, wrap)
	out := sf.out

	for c := 0; c < len(sf.channels); c++ {
		out[c] = lp.Lookup(sf.channels[c])
	}

	return out
}
