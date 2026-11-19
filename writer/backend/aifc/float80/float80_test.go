package float80

import (
	"bytes"
	"math"
	"testing"
)

// the sample rates as AIFF files store them (from the AIFF spec and files of real software)
var aiffRates = []struct {
	rate float64
	b    []byte
}{
	{44100, []byte{0x40, 0x0E, 0xAC, 0x44, 0, 0, 0, 0, 0, 0}},
	{48000, []byte{0x40, 0x0E, 0xBB, 0x80, 0, 0, 0, 0, 0, 0}},
	{22050, []byte{0x40, 0x0D, 0xAC, 0x44, 0, 0, 0, 0, 0, 0}},
	{96000, []byte{0x40, 0x0F, 0xBB, 0x80, 0, 0, 0, 0, 0, 0}},
	{8000, []byte{0x40, 0x0B, 0xFA, 0x00, 0, 0, 0, 0, 0, 0}},
}

func TestBytesAreBinary(t *testing.T) {
	for _, tc := range aiffRates {
		if got := NewFromFloat64(tc.rate).Bytes(); !bytes.Equal(got, tc.b) {
			t.Errorf("Bytes of %v = % X, want % X", tc.rate, got, tc.b)
		}
	}
}

func TestNewFromBytes(t *testing.T) {
	for _, tc := range aiffRates {
		if got := NewFromBytes(tc.b).Float64(); got != tc.rate {
			t.Errorf("NewFromBytes(% X) = %v, want %v", tc.b, got, tc.rate)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	for _, x := range []float64{0, 1, -1, 0.5, 44100, 48000, 11025.5, 1e-5, -123456.789, 1e300, math.Pi, 5e-324} {
		f := NewFromFloat64(x)

		if got := NewFromBytes(f.Bytes()).Float64(); got != x {
			t.Errorf("%v: through bytes %v", x, got)
		}
		if got := NewFromString(f.String()).Float64(); got != x {
			t.Errorf("%v: through string %v", x, got)
		}
	}
}

func TestNewFromBytesLength(t *testing.T) {
	for _, n := range []int{0, 9, 11, 20} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%d bytes must panic", n)
				}
			}()
			NewFromBytes(make([]byte, n))
		}()
	}
}
