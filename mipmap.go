package sndfile

import (
	"github.com/almerlucke/sndfile/dsp/filters"
	"github.com/almerlucke/sndfile/dsp/windows"
	"github.com/almerlucke/sndfile/float"

	"math"
)

type MipMap[T float.Float] struct {
	depth   int
	buffers [][]T
}

func SpeedToMipMapDepth(speed float64) int {
	speed = math.Abs(speed)
	whole, frac := math.Modf(speed)
	depth := int(whole)

	if frac < 0.0001 && depth > 0 {
		depth -= 1
	}

	return depth
}

func NewMipMap[T float.Float](buf []T, sampleRate float64, depth int) (*MipMap[T], error) {
	mm := &MipMap[T]{
		depth:   depth,
		buffers: make([][]T, depth),
	}

	mm.buffers[0] = buf

	fc := sampleRate / 2.0 // Nyquist start

	for d := 1; d < depth; d++ {
		dfc := fc / float64(d+1)
		fir := &filters.FIR[T]{
			Sinc: &filters.Sinc{
				CutOffFreq:   dfc,
				SamplingFreq: int(sampleRate),
				Taps:         200,
				Window:       windows.Hamming,
			},
		}

		filteredBuf, err := fir.LowPass(buf)
		if err != nil {
			return nil, err
		}

		mm.buffers[d] = filteredBuf
	}

	return mm, nil
}

func (mm *MipMap[T]) Length() int {
	return len(mm.buffers[0])
}

func (mm *MipMap[T]) Depth() int {
	return mm.depth
}

func (mm *MipMap[T]) Lookup(pos float64, depth int, wrap bool) T {
	lp := NewLookupParam[T](pos, int64(mm.Length()), wrap)
	return lp.Lookup(mm.buffers[depth])
}

func (mm *MipMap[T]) Buffer(depth int) []T {
	return mm.buffers[depth]
}

type MipMapSoundFile[T float.Float] struct {
	channels      []*MipMap[T]
	sampleRate    float64
	numFrames     int64
	duration      float64
	depth         int
	out           []T
	zeroCrossings []ZeroCrossings
}

func NewMipMapSoundFile[T float.Float](filePath string, depth int) (*MipMapSoundFile[T], error) {
	sndFile, err := NewSoundFile[T](filePath)
	if err != nil {
		return nil, err
	}

	mmsf := &MipMapSoundFile[T]{
		depth:         depth,
		sampleRate:    sndFile.SampleRate(),
		numFrames:     sndFile.NumFrames(),
		duration:      sndFile.Duration(),
		channels:      make([]*MipMap[T], sndFile.NumChannels()),
		out:           make([]T, sndFile.NumChannels()),
		zeroCrossings: sndFile.zeroCrossings,
	}

	for channel := 0; channel < sndFile.NumChannels(); channel++ {
		mm, err := NewMipMap(sndFile.Buffer(channel, 0), mmsf.sampleRate, depth)
		if err != nil {
			return nil, err
		}

		mmsf.channels[channel] = mm
	}

	return mmsf, nil
}

func MustMipMapSoundFile[T float.Float](filePath string, depth int) *MipMapSoundFile[T] {
	sf, err := NewMipMapSoundFile[T](filePath, depth)
	if err != nil {
		panic(err)
	}

	return sf
}

func (sf *MipMapSoundFile[T]) NumChannels() int {
	return len(sf.channels)
}

func (sf *MipMapSoundFile[T]) SampleRate() float64 {
	return sf.sampleRate
}

func (sf *MipMapSoundFile[T]) NumFrames() int64 {
	return sf.numFrames
}

func (sf *MipMapSoundFile[T]) Duration() float64 {
	return sf.duration
}

func (sf *MipMapSoundFile[T]) Depth() int {
	return sf.depth
}

func (sf *MipMapSoundFile[T]) Buffer(channel int, depth int) []T {
	return sf.channels[channel].Buffer(depth)
}

func (sf *MipMapSoundFile[T]) Lookup(pos float64, channel int, depth int, wrap bool) T {
	return sf.channels[channel].Lookup(pos, depth, wrap)
}

func (sf *MipMapSoundFile[T]) LookupAll(pos float64, depth int, wrap bool) []T {
	out := sf.out
	lp := NewLookupParam[T](pos, sf.numFrames, wrap)

	for c := 0; c < len(sf.channels); c++ {
		out[c] = lp.Lookup(sf.channels[c].Buffer(depth))
	}

	return out
}

func (sf *MipMapSoundFile[T]) LookupWithSpeed(pos float64, channel int, speed float64, wrap bool) T {
	return sf.Lookup(pos, channel, SpeedToMipMapDepth(speed), wrap)
}

func (sf *MipMapSoundFile[T]) LookupAllWithSpeed(pos float64, speed float64, wrap bool) []T {
	return sf.LookupAll(pos, SpeedToMipMapDepth(speed), wrap)
}

func (sf *MipMapSoundFile[T]) ZeroCrossings(channel int) ZeroCrossings {
	return sf.zeroCrossings[channel]
}
