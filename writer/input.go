package writer

// InputConverter convert any buffer input type to a float32 buffer output
type InputConverter interface {
	Convert(any) []float32
	FrameSize() int
}

type float interface {
	float32 | float64
}

// ChannelConverter convert from multiple float input buffers (channels) to one interleaved buffer
type ChannelConverter[T float] struct {
	buffer      []float32
	frameSize   int
	numChannels int
}

func NewChannelConverter[T float](frameSize int, numChannels int) *ChannelConverter[T] {
	return &ChannelConverter[T]{
		buffer:      make([]float32, frameSize*numChannels),
		frameSize:   frameSize,
		numChannels: numChannels,
	}
}

func (c *ChannelConverter[T]) Convert(input any) []float32 {
	frames := input.([][]T)

	for i := 0; i < c.frameSize; i++ {
		for j := 0; j < c.numChannels; j++ {
			c.buffer[i*c.numChannels+j] = float32(frames[j][i])
		}
	}

	return c.buffer
}

func (c *ChannelConverter[T]) FrameSize() int {
	return c.frameSize
}

// TypeConverter convert any float buffer (i.e. float64) to []float32
type TypeConverter[T float] struct {
	buffer    []float32
	frameSize int
}

func NewTypeConverter[T float](frameSize int, numChannels int) *TypeConverter[T] {
	return &TypeConverter[T]{
		buffer:    make([]float32, frameSize*numChannels),
		frameSize: frameSize,
	}
}

func (c *TypeConverter[T]) Convert(input any) []float32 {
	for index, samp := range input.([]T) {
		c.buffer[index] = float32(samp)
	}

	return c.buffer
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
	return input.([]float32)
}

func (c *NoConverter) FrameSize() int {
	return c.frameSize
}
