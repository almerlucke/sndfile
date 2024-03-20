package windows

import "math"

var (
	twoPi  = math.Pi * 2
	fourPi = math.Pi * 4
	sixPi  = math.Pi * 6
)

// Function is an alias type representing window functions.
type Function func(int) []float64

// Blackman generates a Blackman window of the requested size
// See https://en.wikipedia.org/wiki/Window_function#Blackman_windows
func Blackman(L int) []float64 {
	r := make([]float64, L)
	LF := float64(L)
	alpha := 0.16
	a0 := (1 - alpha) / 2.0
	a1 := 0.5
	a2 := alpha / 2.0

	for i := 0; i < L; i++ {
		iF := float64(i)
		r[i] = a0 - (a1 * math.Cos((twoPi*iF)/(LF-1))) + (a2 * math.Cos((fourPi*iF)/(LF-1)))
	}
	return r
}

// Hamming generates a Hamming window of the requested size
// See https://en.wikipedia.org/wiki/Window_function#Hamming_window
func Hamming(L int) []float64 {
	r := make([]float64, L)
	alpha := 0.54
	beta := 1.0 - alpha
	Lf := float64(L)

	for i := 0; i < L; i++ {
		r[i] = alpha - (beta * math.Cos((twoPi*float64(i))/(Lf-1)))
	}
	return r
}

// Nuttall generates a Blackman-Nutall window
// See https://en.wikipedia.org/wiki/Window_function#Nuttall_window.2C_continuous_first_derivative
func Nuttall(L int) []float64 {
	r := make([]float64, L)
	LF := float64(L)
	for i := 0; i < L; i++ {
		iF := float64(i)
		r[i] = 0.355768 - 0.487396*math.Cos((twoPi*iF)/(LF-1)) + 0.144232*math.Cos((fourPi*iF)/(LF-1)) - 0.012604*math.Cos((sixPi*iF)/(LF-1))
	}

	return r
}
