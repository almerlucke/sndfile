package backend

import "errors"

// ErrTooLarge is returned by Write when the file would get larger than its format can hold (4 GiB for wav and aiff, the sizes in the header are 32
// bits). Nothing is written then
var ErrTooLarge = errors.New("backend: the file would be larger than 4 GiB, more than the format can hold")

type Backend interface {
	Close() error
	Normalize(float32) error
	Write([]float32) error
}
