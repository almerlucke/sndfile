package main

import (
	"github.com/almerlucke/genny/float/conv"
	"github.com/almerlucke/genny/float/phasor"
	"github.com/almerlucke/genny/float/shape"
	"github.com/almerlucke/genny/float/shape/shapers/mult"
	"github.com/almerlucke/sndfile/writer"
	"github.com/dh1tw/gosamplerate"
	"log"
)

//type Buffer []float64
//
//type InputBuffer struct {
//	conv    *writer.ChannelConverter[float64]
//	buffers [][]float64
//}
//
//func NewInputBuffer(frameSize int, numChannels int) *InputBuffer {
//	return &InputBuffer{
//		conv:    writer.NewChannelConverter[float64](frameSize, numChannels),
//		buffers: make([][]float64, numChannels),
//	}
//}
//
//func (ib *InputBuffer) Convert(input any) []float32 {
//	for i, b := range input.([]Buffer) {
//		ib.buffers[i] = b
//	}
//
//	return ib.conv.Convert(ib.buffers)
//}
//
//func (ib *InputBuffer) FrameSize() int {
//	return ib.conv.FrameSize()
//}

func main() {
	bufSize := 1024
	buffers := make([][]float64, 2)
	buffers[0] = make([]float64, bufSize)
	buffers[1] = make([]float64, bufSize)

	ph1 := shape.New(conv.ToVec(phasor.New(400.0, 88200.0, 0.0)), 1, mult.New(0.4))
	ph2 := shape.New(conv.ToVec(phasor.New(300.0, 88200.0, 0.0)), 1, mult.New(0.4))

	sf, err := writer.NewWithOptions(
		"test.wav",
		writer.WAV,
		2,
		44100.0,
		writer.Options{
			InputConverter:    writer.NewChannelConverter[float64](bufSize, 2),
			ConvertSampleRate: true,
			InputSampleRate:   88200.0,
			SrConvQuality:     gosamplerate.SRC_SINC_BEST_QUALITY,
			Normalize:         true,
		},
	)
	if err != nil {
		log.Fatalf("sf error: %v", err)
	}

	defer func() {
		_ = sf.Close()
	}()

	for i := range 80 {
		for j := range 1024 {
			buffers[0][j] = ph1.Generate()[0]
			buffers[1][j] = ph2.Generate()[0]
		}

		var end bool

		if i == 79 {
			end = true
		}

		err = sf.Write(buffers, end)
		if err != nil {
			log.Fatalf("sf error: %v", err)
		}
	}
}
