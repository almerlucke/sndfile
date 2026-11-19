//go:build srcompare

package resample

/*
#cgo pkg-config: samplerate
#include <samplerate.h>
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"unsafe"
)

// libSRC calls libsamplerate directly and drains it completely, as the reference for the comparison tests. Only built with the srcompare tag.
type libSRC struct {
	state    *C.SRC_STATE
	channels int
}

func newLibSRC(quality, channels int) (*libSRC, error) {
	var errNo C.int
	st := C.src_new(C.int(quality), C.int(channels), &errNo)
	if st == nil {
		return nil, errors.New("src_new failed")
	}
	return &libSRC{state: st, channels: channels}, nil
}

func (l *libSRC) close() { C.src_delete(l.state) }

// process feeds all of in and gives all the output. At the end of input it keeps calling until the filter has nothing left
func (l *libSRC) process(in []float32, ratio float64, end bool) ([]float32, error) {
	const outFrames = 4096
	ch := l.channels

	cin := (*C.float)(C.malloc(C.size_t(max(len(in), 1)) * 4))
	cout := (*C.float)(C.malloc(C.size_t(outFrames*ch) * 4))
	defer C.free(unsafe.Pointer(cin))
	defer C.free(unsafe.Pointer(cout))

	copy(unsafe.Slice((*float32)(unsafe.Pointer(cin)), max(len(in), 1)), in)

	var out []float32
	var data C.SRC_DATA
	data.src_ratio = C.double(ratio)

	left := len(in) / ch
	pos := 0

	for {
		data.data_in = (*C.float)(unsafe.Add(unsafe.Pointer(cin), pos*ch*4))
		data.data_out = cout
		data.input_frames = C.long(left)
		data.output_frames = C.long(outFrames)
		if end {
			data.end_of_input = 1
		} else {
			data.end_of_input = 0
		}

		if res := C.src_process(l.state, &data); res != 0 {
			return nil, errors.New(C.GoString(C.src_strerror(res)))
		}

		got := int(data.output_frames_gen)
		out = append(out, unsafe.Slice((*float32)(unsafe.Pointer(cout)), got*ch)...)

		used := int(data.input_frames_used)
		pos += used
		left -= used

		if left == 0 && got < outFrames {
			break
		}
		if used == 0 && got == 0 {
			break
		}
	}

	return out, nil
}
