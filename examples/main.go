package main

import (
	"github.com/almerlucke/genny/float/phasor"
	"github.com/almerlucke/sndfile/writer"
	"log"
)

type Buffer []float64

type InputBuffer struct {
	conv    *writer.ChannelConverter[float64]
	buffers [][]float64
}

func NewInputBuffer(frameSize int, numChannels int) *InputBuffer {
	return &InputBuffer{
		conv:    writer.NewChannelConverter[float64](frameSize, numChannels),
		buffers: make([][]float64, numChannels),
	}
}

func (ib *InputBuffer) Convert(input any) []float32 {
	for i, b := range input.([]Buffer) {
		ib.buffers[i] = b
	}

	return ib.conv.Convert(ib.buffers)
}

func (ib *InputBuffer) FrameSize() int {
	return ib.conv.FrameSize()
}

func main() {
	bufSize := 1024
	buffers := make([]Buffer, 2)
	buffers[0] = make(Buffer, bufSize)
	buffers[1] = make(Buffer, bufSize)

	ph1 := phasor.New(400.0, 44100.0, 0.0)
	ph2 := phasor.New(300.0, 44100.0, 0.0)

	sf, err := writer.New(
		"test.aiff",
		writer.AIFC,
		2,
		44100.0,
		NewInputBuffer(bufSize, 2),
	)
	if err != nil {
		log.Fatalf("sf error: %v", err)
	}

	defer func() {
		_ = sf.Close()
	}()

	for _ = range 40 {
		for j := range 1024 {
			buffers[0][j] = ph1.Generate()
			buffers[1][j] = ph2.Generate()
		}

		err = sf.Write(buffers, true)
		if err != nil {
			log.Fatalf("sf error: %v", err)
		}
	}
}
