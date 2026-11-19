package writer

import (
	"fmt"

	"github.com/almerlucke/sndfile/float"
)

// InputConverter convert any buffer input type to a float32 buffer output
type InputConverter interface {
	Convert(any) []float32
	FrameSize() int
}

// CheckedConverter is an InputConverter that reports a wrong input (of another type, or with another number of channels) as an error: a Writer
// uses ConvertChecked if the converter has it, and Convert (that panics on such an input) if not. The converters of this package have it
type CheckedConverter interface {
	InputConverter
	ConvertChecked(any) ([]float32, error)
}

// mustConvert is Convert for a converter that has only ConvertChecked
func mustConvert(out []float32, err error) []float32 {
	if err != nil {
		panic(err)
	}

	return out
}

// ChannelConverter convert from multiple float input buffers (channels) to one interleaved buffer. The input is a [][]T with a buffer per channel.
// A block can be shorter or longer than the frame size: the output is as long as the shortest channel
type ChannelConverter[T float.Float] struct {
	buffer      []float32
	frameSize   int
	numChannels int
}

func NewChannelConverter[T float.Float](frameSize int, numChannels int) *ChannelConverter[T] {
	return &ChannelConverter[T]{
		buffer:      make([]float32, frameSize*numChannels),
		frameSize:   frameSize,
		numChannels: numChannels,
	}
}

func (c *ChannelConverter[T]) Convert(input any) []float32 {
	return mustConvert(c.ConvertChecked(input))
}

func (c *ChannelConverter[T]) ConvertChecked(input any) ([]float32, error) {
	frames, ok := input.([][]T)
	if !ok {
		return nil, fmt.Errorf("writer: channel converter wants %T, got %T", frames, input)
	}

	if len(frames) != c.numChannels {
		return nil, fmt.Errorf("writer: channel converter has %d channels, got a block of %d", c.numChannels, len(frames))
	}

	n := 0
	if len(frames) > 0 {
		n = len(frames[0])
		for _, ch := range frames[1:] {
			n = min(n, len(ch))
		}
	}

	if cap(c.buffer) < n*c.numChannels {
		c.buffer = make([]float32, n*c.numChannels)
	}
	out := c.buffer[:n*c.numChannels]

	for i := 0; i < n; i++ {
		for j := 0; j < c.numChannels; j++ {
			out[i*c.numChannels+j] = float32(frames[j][i])
		}
	}

	return out, nil
}

func (c *ChannelConverter[T]) FrameSize() int {
	return c.frameSize
}

// TypeConverter convert any float buffer (i.e. float64) to []float32. The input is a []T of interleaved samples of any length
type TypeConverter[T float.Float] struct {
	buffer    []float32
	frameSize int
}

func NewTypeConverter[T float.Float](frameSize int, numChannels int) *TypeConverter[T] {
	return &TypeConverter[T]{
		buffer:    make([]float32, frameSize*numChannels),
		frameSize: frameSize,
	}
}

func (c *TypeConverter[T]) Convert(input any) []float32 {
	return mustConvert(c.ConvertChecked(input))
}

func (c *TypeConverter[T]) ConvertChecked(input any) ([]float32, error) {
	in, ok := input.([]T)
	if !ok {
		return nil, fmt.Errorf("writer: type converter wants %T, got %T", in, input)
	}

	if cap(c.buffer) < len(in) {
		c.buffer = make([]float32, len(in))
	}
	out := c.buffer[:len(in)]

	for index, samp := range in {
		out[index] = float32(samp)
	}

	return out, nil
}

func (c *TypeConverter[T]) FrameSize() int {
	return c.frameSize
}

// NoConverter just passes the input to the output as []float32
type NoConverter struct {
	frameSize int
}

func NewNoConverter(frameSize int) *NoConverter {
	return &NoConverter{
		frameSize: frameSize,
	}
}

func (c *NoConverter) Convert(input any) []float32 {
	return mustConvert(c.ConvertChecked(input))
}

func (c *NoConverter) ConvertChecked(input any) ([]float32, error) {
	in, ok := input.([]float32)
	if !ok {
		return nil, fmt.Errorf("writer: no converter wants []float32, got %T", input)
	}

	return in, nil
}

func (c *NoConverter) FrameSize() int {
	return c.frameSize
}
