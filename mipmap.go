package sndfile

import (
	"github.com/almerlucke/sndfile/dsp/filters"
	"github.com/almerlucke/sndfile/dsp/windows"
	"math"
)

type MipMap struct {
	depth   int
	buffers [][]float64
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

func NewMipMap(buf []float64, sampleRate float64, depth int) (*MipMap, error) {
	mm := &MipMap{
		depth:   depth,
		buffers: make([][]float64, depth),
	}

	mm.buffers[0] = buf

	fc := sampleRate / 2.0 // Nyquist start

	for d := 1; d < depth; d++ {
		dfc := fc / float64(d+1)
		fir := &filters.FIR{
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

func (mm *MipMap) Length() int {
	return len(mm.buffers[0])
}

func (mm *MipMap) Depth() int {
	return mm.depth
}

func (mm *MipMap) Lookup(pos float64, depth int, wrap bool) float64 {
	lp := NewLookupParam(pos, int64(mm.Length()), wrap)
	return lp.Lookup(mm.buffers[depth])
}

func (mm *MipMap) Buffer(depth int) []float64 {
	return mm.buffers[depth]
}

type MipMapSoundFile struct {
	channels   []*MipMap
	sampleRate float64
	numFrames  int64
	duration   float64
	depth      int
	out        []float64
}

func NewMipMapSoundFile(filePath string, depth int) (*MipMapSoundFile, error) {
	sndFile, err := NewSoundFile(filePath)
	if err != nil {
		return nil, err
	}

	mmsf := &MipMapSoundFile{
		depth:      depth,
		sampleRate: sndFile.SampleRate(),
		numFrames:  sndFile.NumFrames(),
		duration:   sndFile.Duration(),
		channels:   make([]*MipMap, sndFile.NumChannels()),
		out:        make([]float64, sndFile.NumChannels()),
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

func (mmsf *MipMapSoundFile) NumChannels() int {
	return len(mmsf.channels)
}

func (mmsf *MipMapSoundFile) SampleRate() float64 {
	return mmsf.sampleRate
}

func (mmsf *MipMapSoundFile) NumFrames() int64 {
	return mmsf.numFrames
}

func (mmsf *MipMapSoundFile) Duration() float64 {
	return mmsf.duration
}

func (mmsf *MipMapSoundFile) Depth() int {
	return mmsf.depth
}

func (mmsf *MipMapSoundFile) Buffer(channel int, depth int) []float64 {
	return mmsf.channels[channel].Buffer(depth)
}

func (mmsf *MipMapSoundFile) Lookup(pos float64, channel int, depth int, wrap bool) float64 {
	return mmsf.channels[channel].Lookup(pos, depth, wrap)
}

func (mmsf *MipMapSoundFile) LookupAll(pos float64, depth int, wrap bool) []float64 {
	out := mmsf.out
	lp := NewLookupParam(pos, mmsf.numFrames, wrap)

	for c := 0; c < len(mmsf.channels); c++ {
		out[c] = lp.Lookup(mmsf.channels[c].Buffer(depth))
	}

	return out
}

func (mmsf *MipMapSoundFile) LookupWithSpeed(pos float64, channel int, speed float64, wrap bool) float64 {
	return mmsf.Lookup(pos, channel, SpeedToMipMapDepth(speed), wrap)
}

func (mmsf *MipMapSoundFile) LookupAllWithSpeed(pos float64, speed float64, wrap bool) []float64 {
	return mmsf.LookupAll(pos, SpeedToMipMapDepth(speed), wrap)
}
