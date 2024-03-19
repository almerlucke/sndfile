package writer

import (
	"errors"
	"github.com/almerlucke/sndfile/writer/backend"
	"github.com/almerlucke/sndfile/writer/backend/aifc"
	"github.com/almerlucke/sndfile/writer/backend/wav"
	"github.com/dh1tw/gosamplerate"
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
	SrConvQuality     int
	InputSampleRate   float64
	Normalize         bool
}

type Writer struct {
	opt         Options
	srConv      gosamplerate.Src
	backend     backend.Backend
	numChannels int
	srRatio     float64
	max         float32
}

func New(filePath string, fileFormat FileFormat, numChannels int, sampleRate float64, inputConv InputConverter) (*Writer, error) {
	return NewWithOptions(filePath, fileFormat, numChannels, sampleRate, Options{
		InputConverter: inputConv,
	})
}

func NewWithOptions(filePath string, fileFormat FileFormat, numChannels int, sampleRate float64, opt Options) (*Writer, error) {
	var be backend.Backend
	var err error

	switch fileFormat {
	case AIFC:
		be, err = aifc.New(filePath, numChannels, sampleRate)
		if err != nil {
			return nil, err
		}
	case WAV:
		be, err = wav.New(filePath, numChannels, sampleRate)
		if err != nil {
			return nil, err
		}
	}

	return NewWithBackend(be, numChannels, sampleRate, opt)
}

func NewWithBackend(be backend.Backend, numChannels int, sampleRate float64, opt Options) (*Writer, error) {
	w := &Writer{
		opt:         opt,
		numChannels: numChannels,
		backend:     be,
	}

	if opt.InputConverter == nil {
		return nil, errors.New("input converter option should not be nil")
	}

	if opt.ConvertSampleRate {
		frameSize := DefaultFrameSize

		if opt.InputConverter.FrameSize() != 0 {
			frameSize = opt.InputConverter.FrameSize()
		}

		srConv, err := gosamplerate.New(opt.SrConvQuality, numChannels, frameSize*numChannels)
		if err != nil {
			return nil, err
		}

		w.srConv = srConv
		w.srRatio = sampleRate / opt.InputSampleRate

		err = w.srConv.SetRatio(w.srRatio)
		if err != nil {
			return nil, err
		}
	}

	return w, nil
}

func (wr *Writer) Write(input any, endOfInput bool) error {
	var err error

	if wr.opt.InputConverter == nil {
		return errors.New("frame converter option should not be nil")
	}

	output := wr.opt.InputConverter.Convert(input)

	for _, samp := range output {
		if samp > wr.max {
			wr.max = samp
		}
	}

	if wr.opt.ConvertSampleRate {
		output, err = wr.srConv.Process(output, wr.srRatio, endOfInput)
		if err != nil {
			return err
		}
	}

	if len(output) > 0 {
		err = wr.backend.Write(output)
		if err != nil {
			return err
		}
	}

	return nil
}

func (wr *Writer) Close() error {
	var errs []error
	var err error

	if wr.opt.Normalize {
		err = wr.backend.Normalize(wr.max)
		if err != nil {
			errs = append(errs, err)
		}
	}

	if wr.opt.ConvertSampleRate {
		err = gosamplerate.Delete(wr.srConv)
		if err != nil {
			errs = append(errs, err)
		}
	}

	err = wr.backend.Close()
	if err != nil {
		errs = append(errs, err)
	}

	if len(errs) != 0 {
		return errors.Join(errs...)
	}

	return nil
}
