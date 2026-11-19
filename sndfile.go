package sndfile

import (
	"errors"
	"fmt"

	"github.com/almerlucke/sndfile/float"
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

// NewSoundFile load sound file from disk. The pure-Go decoders read wav, aiff, mp3, flac and ogg vorbis. For other formats, build with
// -tags libsndfile (needs cgo and the libsndfile library): they are then read by libsndfile

func NewSoundFile[T float.Float](filePath string) (*SoundFile[T], error) {
	// Try the pure-Go decoders (wav, aiff, mp3, flac, ogg vorbis) first. Other formats and encodings (a-law, adpcm, au, caf, w64, rf64, opus, ...) need
	// a build with the libsndfile tag, see sndfile_libsndfile.go
	dec, err := decodeFile[T](filePath)
	if err == nil {
		return newSoundFileFromDecoded[T](dec), nil
	}

	if !errors.Is(err, errNoDecoder) {
		return nil, fmt.Errorf("decode %s: %w", filePath, err)
	}

	return newSoundFileLibsndfile[T](filePath)
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

func newSoundFileFromDecoded[T float.Float](dec *decoded[T]) *SoundFile[T] {
	channels := dec.channels
	zeroCrossings := make([]ZeroCrossings, len(channels))

	for c := range channels {
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
