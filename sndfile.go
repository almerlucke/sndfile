package sndfile

import (
	"errors"
	"fmt"

	"github.com/almerlucke/sndfile/float"
	"github.com/mkb218/gosndfile/sndfile"
)

type SoundFiler[T float.Float] interface {
	NumChannels() int
	SampleRate() float64
	NumFrames() int64
	Duration() float64
	Depth() int
	Buffer(channel int, depth int) []T
	Lookup(pos float64, channel int, depth int, wrap bool) T
	LookupAll(pos float64, depth int, wrap bool) []T
	ZeroCrossings(channel int) ZeroCrossings
}

// SoundFile contains sound file deinterleaved samples and implements SoundFiler interface
type SoundFile[T float.Float] struct {
	// Deinterleaved channels
	channels [][]T
	// Sample rate
	sampleRate float64
	// Number of frames
	numFrames int64
	// Duration in seconds
	duration float64
	// Lookup output
	out []T
	// Zero crossings
	zeroCrossings []ZeroCrossings
}

// NewSoundFile load sound file from disk
func NewSoundFile[T float.Float](filePath string) (*SoundFile[T], error) {
	// Try the pure-Go decoders (mp3, flac, ogg vorbis) first, fall back to libsndfile
	dec, err := decodeFile(filePath)
	if err == nil {
		return newSoundFileFromDecoded[T](dec), nil
	}

	if !errors.Is(err, errNoDecoder) {
		return nil, fmt.Errorf("decode %s: %w", filePath, err)
	}

	info := sndfile.Info{}

	file, err := sndfile.Open(filePath, sndfile.Read, &info)
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = file.Close()
	}()

	// Create one big buffer to hold all samples
	fileBuffer := make([]T, int64(info.Channels)*info.Frames)

	// Create separate channels by splitting buffer into info.Channels parts
	channels := make([][]T, info.Channels)
	for i := int32(0); i < info.Channels; i++ {
		channels[i] = fileBuffer[int64(i)*info.Frames : int64(i+1)*info.Frames]
	}

	// Deinterleave in blocks
	sampleBlockSize := int64(2048) * int64(info.Channels)
	samples := make([]T, sampleBlockSize)
	frameIndex := int64(0)

	for {
		framesRead, err := file.ReadFrames(samples)
		if err != nil {
			return nil, err
		}

		if framesRead == 0 {
			break
		}

		for i := range framesRead {
			for j := int64(0); j < int64(info.Channels); j++ {
				channels[j][frameIndex+i] = samples[i*int64(info.Channels)+j]
			}
		}

		frameIndex += framesRead
	}

	// Find zero crossings
	zeroCrossings := make([]ZeroCrossings, info.Channels)
	for i := range zeroCrossings {
		zeroCrossings[i] = calculateZeroCrossings(channels[i])
	}

	sf := SoundFile[T]{}
	sf.duration = float64(info.Frames) / float64(info.Samplerate)
	sf.numFrames = info.Frames
	sf.channels = channels
	sf.zeroCrossings = zeroCrossings
	sf.sampleRate = float64(info.Samplerate)
	sf.out = make([]T, info.Channels)

	return &sf, nil
}

// MustSoundFile must load a sound file
func MustSoundFile[T float.Float](filePath string) *SoundFile[T] {
	sf, err := NewSoundFile[T](filePath)
	if err != nil {
		panic(err)
	}

	return sf
}

func (sf *SoundFile[T]) NumChannels() int {
	return len(sf.channels)
}

func (sf *SoundFile[T]) SampleRate() float64 {
	return sf.sampleRate
}

func (sf *SoundFile[T]) NumFrames() int64 {
	return sf.numFrames
}

func (sf *SoundFile[T]) Duration() float64 {
	return sf.duration
}

func (sf *SoundFile[T]) Depth() int {
	return 1
}

func (sf *SoundFile[T]) Buffer(channel int, _ int) []T {
	return sf.channels[channel]
}

func (sf *SoundFile[T]) Lookup(pos float64, channel int, _ int, wrap bool) T {
	lp := NewLookupParam[T](pos, sf.numFrames, wrap)
	return lp.Lookup(sf.channels[channel])
}

func (sf *SoundFile[T]) LookupAll(pos float64, _ int, wrap bool) []T {
	lp := NewLookupParam[T](pos, sf.numFrames, wrap)
	out := sf.out

	for c := 0; c < len(sf.channels); c++ {
		out[c] = lp.Lookup(sf.channels[c])
	}

	return out
}

func (sf *SoundFile[T]) ZeroCrossings(channel int) ZeroCrossings {
	return sf.zeroCrossings[channel]
}

func newSoundFileFromDecoded[T float.Float](dec *decoded) *SoundFile[T] {
	channels := make([][]T, len(dec.channels))
	zeroCrossings := make([]ZeroCrossings, len(dec.channels))

	for c, src := range dec.channels {
		channels[c] = make([]T, len(src))
		for i, v := range src {
			channels[c][i] = T(v)
		}

		zeroCrossings[c] = calculateZeroCrossings(channels[c])
	}

	numFrames := int64(0)
	if len(channels) > 0 {
		numFrames = int64(len(channels[0]))
	}

	return &SoundFile[T]{
		channels:      channels,
		sampleRate:    float64(dec.sampleRate),
		numFrames:     numFrames,
		duration:      float64(numFrames) / float64(dec.sampleRate),
		out:           make([]T, len(channels)),
		zeroCrossings: zeroCrossings,
	}
}
