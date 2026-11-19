package writer

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"

	"github.com/almerlucke/sndfile/dsp/resample"
	"github.com/almerlucke/sndfile/writer/backend"
	"github.com/almerlucke/sndfile/writer/backend/aifc"
	"github.com/almerlucke/sndfile/writer/backend/wav"
)

type FileFormat int

const (
	AIFC FileFormat = iota
	WAV
)

const (
	DefaultFrameSize = 8192
)

type Options struct {
	InputConverter    InputConverter
	ConvertSampleRate bool
	// SrConvQuality is one of the qualities of package resample (resample.SincBest, the default, ... resample.Linear), the numbers are the
	// same as those of libsamplerate
	SrConvQuality   int
	InputSampleRate float64
	Normalize       bool
}

type Writer struct {
	opt         Options
	srConv      *resample.Resampler
	backend     backend.Backend
	numChannels int
	srRatio     float64
	// max is the largest absolute value of the samples that are written, that is after the sample rate conversion: the filter can overshoot
	max float32
	// ended is true when the stream has been finished by a Write with endOfInput, until another Write follows
	ended  bool
	closed bool
}

func New(filePath string, fileFormat FileFormat, numChannels int, sampleRate float64, inputConv InputConverter) (*Writer, error) {
	return NewWithOptions(filePath, fileFormat, numChannels, sampleRate, Options{
		InputConverter: inputConv,
	})
}

func NewWithOptions(filePath string, fileFormat FileFormat, numChannels int, sampleRate float64, opt Options) (*Writer, error) {
	var be backend.Backend
	var err error

	// check before a file is made
	if numChannels < 1 {
		return nil, fmt.Errorf("writer: number of channels should be at least 1, got %d", numChannels)
	}

	ext := filepath.Ext(filePath)

	switch fileFormat {
	case AIFC:
		if ext == "" {
			filePath += ".aif"
		}
		be, err = aifc.New(filePath, numChannels, sampleRate)
		if err != nil {
			return nil, err
		}
	case WAV:
		if ext == "" {
			filePath += ".wav"
		}
		be, err = wav.New(filePath, numChannels, sampleRate)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("writer: unknown file format %d", fileFormat)
	}

	w, err := NewWithBackend(be, numChannels, sampleRate, opt)
	if err != nil {
		// do not leave an open file with a header behind
		_ = be.Close()
		return nil, err
	}

	return w, nil
}

func NewWithBackend(be backend.Backend, numChannels int, sampleRate float64, opt Options) (*Writer, error) {
	if be == nil {
		return nil, errors.New("writer: backend should not be nil")
	}

	if numChannels < 1 {
		return nil, fmt.Errorf("writer: number of channels should be at least 1, got %d", numChannels)
	}

	if opt.InputConverter == nil {
		return nil, errors.New("input converter option should not be nil")
	}

	w := &Writer{
		opt:         opt,
		numChannels: numChannels,
		backend:     be,
	}

	if opt.ConvertSampleRate {
		if !(sampleRate > 0) || !(opt.InputSampleRate > 0) {
			return nil, fmt.Errorf("writer: sample rate conversion needs positive rates, got input %v and output %v", opt.InputSampleRate, sampleRate)
		}

		srConv, err := resample.New(opt.SrConvQuality, numChannels)
		if err != nil {
			return nil, err
		}

		w.srRatio = sampleRate / opt.InputSampleRate

		err = srConv.SetRatio(w.srRatio)
		if err != nil {
			return nil, err
		}

		w.srConv = srConv
	}

	return w, nil
}

// Write converts input with the input converter, converts the sample rate if that is asked for and writes the result. Give endOfInput on the last
// Write so that the sample rate converter is finished: if you forget, Close does it. The input may be shorter than one frame size of the converter
// (the last block of a render), but it has to be whole frames.
func (wr *Writer) Write(input any, endOfInput bool) error {
	if wr.closed {
		return errors.New("writer: write after close")
	}

	var output []float32

	if cc, ok := wr.opt.InputConverter.(CheckedConverter); ok {
		var err error

		output, err = cc.ConvertChecked(input)
		if err != nil {
			return err
		}
	} else {
		output = wr.opt.InputConverter.Convert(input)
	}

	if len(output)%wr.numChannels != 0 {
		return fmt.Errorf("writer: input of %d samples is not whole frames of %d channels", len(output), wr.numChannels)
	}

	if wr.opt.ConvertSampleRate {
		var err error

		output, err = wr.srConv.Process(output, endOfInput)
		if err != nil {
			return err
		}
	}

	wr.ended = endOfInput

	return wr.put(output)
}

// put writes samples to the backend and keeps the peak of what is written
func (wr *Writer) put(samples []float32) error {
	for _, samp := range samples {
		if a := float32(math.Abs(float64(samp))); a > wr.max {
			wr.max = a
		}
	}

	if len(samples) == 0 {
		return nil
	}

	return wr.backend.Write(samples)
}

// Close finishes the sample rate conversion if that was not done, normalizes if that is asked for, and closes the file. It is safe to call twice
func (wr *Writer) Close() error {
	if wr.closed {
		return nil
	}
	wr.closed = true

	var errs []error

	if wr.opt.ConvertSampleRate && !wr.ended {
		// the converter still holds the last frames of the sound
		output, err := wr.srConv.Process(nil, true)
		if err == nil {
			err = wr.put(output)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}

	if wr.opt.Normalize {
		if err := wr.backend.Normalize(wr.max); err != nil {
			errs = append(errs, err)
		}
	}

	if err := wr.backend.Close(); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}
