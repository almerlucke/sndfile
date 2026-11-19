// Package resample is a pure-Go streaming sample rate converter.
//
// The sinc converters use a Kaiser windowed sinc, tabulated once and linearly interpolated, the same method as libsamplerate. When the rate is lowered
// the filter is stretched so that it low passes below the new Nyquist frequency first, nothing aliases. The converters are numbered like the
// converters of libsamplerate, so a quality that was a libsamplerate constant keeps its meaning.
//
// # Compared with libsamplerate
//
// The designs were tuned against libsamplerate 0.2.2 on a sine that is converted and compared with the exact sine at the new rate (error relative to
// the signal, lower is better), on 44.1 to 48 kHz, 48 to 44.1 kHz, 44.1 to 96 kHz and 96 to 44.1 kHz:
//
//	           pure Go          libsamplerate
//	best       -146 .. -148 dB  -137 .. -144 dB
//	medium     -130 .. -142 dB  -117 .. -126 dB
//	fastest    -111 .. -114 dB   -98 .. -103 dB
//
// The error of best is at the precision of float32. A tone at 0.45 of the sample rate when the rate is halved comes out at -160 dB (best), against
// -150 dB. The transition band is as steep: at 1.005 times the Nyquist frequency best is at -152 dB, libsamplerate at -155 dB. The weights of a ratio
// that is a fraction of small numbers (160/147 for 44.1 to 48 kHz) are made once per phase, and that makes the converter 1.5 to 2 times faster than
// libsamplerate on an i7-1280P (4 seconds of stereo at best: 80 ms against 159 ms). A ratio that is no small fraction (pi/3) is about 2 times slower
// (293 ms against 144 ms).
//
// The comparison itself (a cgo test with a build tag, that called libsamplerate directly) was removed so that the module does not need libsamplerate
// or cgo to be tested. It is in the history: git show 6d8726d:dsp/resample/srcompare_test.go and srcompare_cgo.go.
package resample

import (
	"errors"
	"fmt"
	"math"
	"sync"
)

// Qualities, the numbers are those of libsamplerate (SRC_SINC_BEST_QUALITY, ...)
const (
	SincBest      = 0 // long filter, over 140 dB stop band, transition band of 3% of the Nyquist frequency
	SincMedium    = 1
	SincFastest   = 2
	ZeroOrderHold = 3 // no filter at all, repeats the last sample
	Linear        = 4 // no filter at all, linear interpolation
)

// design is the shape of a sinc converter
type design struct {
	// zeroCrossings on each side of the center, in units of the cutoff
	zeroCrossings int
	// perCrossing is the number of table entries between two zero crossings
	perCrossing int
	// beta is the shape of the Kaiser window, more is more stop band attenuation and a wider transition
	beta float64
	// bandwidth is the part of the Nyquist frequency that passes
	bandwidth float64
}

var designs = map[int]design{
	// the numbers were tuned against libsamplerate: the same steepness of the transition band, the same or less error in the pass band
	SincBest:    {zeroCrossings: 176, perCrossing: 4096, beta: 16.5, bandwidth: 0.97},
	SincMedium:  {zeroCrossings: 48, perCrossing: 1024, beta: 12.0, bandwidth: 0.93},
	SincFastest: {zeroCrossings: 20, perCrossing: 512, beta: 10.0, bandwidth: 0.88},
}

// Resampler converts interleaved float32 frames of a fixed number of channels with a ratio that is set up front. It is for one stream, not safe for
// use from more than one goroutine.
type Resampler struct {
	quality  int
	channels int
	ratio    float64 // output rate / input rate

	// sinc state
	table []float64 // sinc * window from the center out, one entry extra for the interpolation
	d     design
	scale float64 // cutoff relative to the input rate, 1 is Nyquist of the input
	half  int     // taps on each side of the center, in input frames

	// a ratio of two small whole numbers (160/147 for 44.1k to 48k) repeats its positions after ratioQ input frames and ratioP output frames: the
	// weights of the ratioP different phases are made once. ratioP is 0 if the ratio is no such fraction
	ratioP, ratioQ int64
	phases         [][]float64

	// stream state
	buf     []float32 // interleaved input frames, the first is the frame with index base
	base    int64     // index of the first buffered input frame, negative for the zeros before the start
	total   int64     // number of input frames received
	nextOut int64     // index of the next output frame

	coefs []float64
	out   []float32
}

// New makes a resampler for a quality and number of channels. Call SetRatio before Process
func New(quality, channels int) (*Resampler, error) {
	if channels < 1 {
		return nil, errors.New("resample: channels must be at least 1")
	}

	r := &Resampler{quality: quality, channels: channels, ratio: 1}

	switch quality {
	case SincBest, SincMedium, SincFastest:
		r.d = designs[quality]
		r.table = cachedSincTable(r.d)
	case ZeroOrderHold, Linear:
	default:
		return nil, fmt.Errorf("resample: unknown quality %d", quality)
	}

	if err := r.SetRatio(1); err != nil {
		return nil, err
	}

	return r, nil
}

// SetRatio sets the ratio output rate / input rate, between 1/256 and 256. Do this before the first Process (or after Reset): a ratio that changes
// in the middle of a stream is not supported
func (r *Resampler) SetRatio(ratio float64) error {
	if !(ratio >= 1.0/256 && ratio <= 256) || math.IsNaN(ratio) {
		return fmt.Errorf("resample: ratio %v out of range", ratio)
	}

	r.ratio = ratio

	switch r.quality {
	case SincBest, SincMedium, SincFastest:
		r.scale = r.d.bandwidth * math.Min(1, ratio)
		r.half = int(math.Ceil(float64(r.d.zeroCrossings) / r.scale))
	case ZeroOrderHold, Linear:
		r.half = 1
	}

	r.coefs = make([]float64, 2*r.half)
	r.ratioP, r.ratioQ, r.phases = 0, 0, nil

	if p, q, ok := fraction(ratio, maxPhases); ok && int64(len(r.coefs))*p <= maxPhaseCoefs {
		r.ratioP, r.ratioQ = p, q
		r.phases = make([][]float64, p)
	}

	r.Reset()

	return nil
}

const (
	// maxPhases is the most phases of a fraction ratio that are made up front
	maxPhases = 4096
	// maxPhaseCoefs limits the weights that are kept: 4M weights is 32 MB
	maxPhaseCoefs = 4 << 20
)

// fraction finds p/q (p at most maxP) that is the ratio to within 1e-12, with the continued fraction of the ratio
func fraction(ratio float64, maxP int64) (p, q int64, ok bool) {
	h0, h1 := int64(0), int64(1) // numerators
	k0, k1 := int64(1), int64(0) // denominators
	x := ratio

	for i := 0; i < 40; i++ {
		a := int64(math.Floor(x))
		h0, h1 = h1, a*h1+h0
		k0, k1 = k1, a*k1+k0

		if h1 > maxP {
			return 0, 0, false
		}

		if math.Abs(float64(h1)/float64(k1)-ratio) <= ratio*1e-12 {
			return h1, k1, true
		}

		frac := x - float64(a)
		if frac < 1e-15 {
			return 0, 0, false
		}
		x = 1 / frac
	}

	return 0, 0, false
}

// position gives output frame n as a place in the input: i0 is the input frame at or before it, frac how far after i0 it is and phase (only for a
// fraction ratio, otherwise -1) the number of the weights that are the same for every frame with that phase
func (r *Resampler) position(n int64) (i0 int64, frac float64, phase int) {
	if r.ratioP > 0 {
		num := n * r.ratioQ
		ph := num % r.ratioP
		return num / r.ratioP, float64(ph) / float64(r.ratioP), int(ph)
	}

	p := float64(n) / r.ratio
	i0 = int64(math.Floor(p))
	return i0, p - float64(i0), -1
}

// weights are the weights for an output at a phase or fraction after input frame i0: they are in r.coefs or in the phase cache
func (r *Resampler) weights(frac float64, phase int) []float64 {
	if phase < 0 {
		r.fillCoefs(r.coefs, frac)
		return r.coefs
	}

	if r.phases[phase] == nil {
		c := make([]float64, len(r.coefs))
		r.fillCoefs(c, frac)
		r.phases[phase] = c
	}

	return r.phases[phase]
}

// Reset forgets the stream so that the resampler can be used for a new one
func (r *Resampler) Reset() {
	r.half = max(r.half, 1)
	// the frames before the start are zero
	r.base = -int64(r.half)
	r.buf = make([]float32, r.half*r.channels)
	r.total = 0
	r.nextOut = 0
}

// Latency is the number of input frames the filter looks ahead
func (r *Resampler) Latency() int { return r.half }

// Process takes interleaved input frames and gives all the output frames that can be made from them, interleaved. A sinc filter needs frames after
// the one that it makes: what it still lacks stays kept until more input comes, or until endOfInput is true, when the stream is finished with
// zeros and the resampler is reset. The result is valid until the next call.
func (r *Resampler) Process(in []float32, endOfInput bool) ([]float32, error) {
	if len(in)%r.channels != 0 {
		return nil, fmt.Errorf("resample: input of %d samples is not whole frames of %d channels", len(in), r.channels)
	}

	r.buf = append(r.buf, in...)
	r.total += int64(len(in) / r.channels)
	r.out = r.out[:0]

	ch := r.channels
	avail := r.base + int64(len(r.buf)/ch) // the frames before this index are buffered

	for {
		i0, frac, phase := r.position(r.nextOut)

		if endOfInput {
			if i0 >= r.total {
				break
			}
		} else if i0+int64(r.rightTaps()) >= avail {
			break
		}

		lo := i0 - int64(r.half) + 1
		coefs := r.weights(frac, phase)

		if lo >= r.base && lo+int64(len(coefs)) <= avail {
			// all the frames are buffered: no checks per frame
			r.dot(coefs, int(lo-r.base)*ch)
		} else {
			for c := 0; c < ch; c++ {
				var sum float64

				for k, w := range coefs {
					j := lo + int64(k)
					if j < r.base || j >= avail { // before the start or beyond the end of the stream: zeros
						continue
					}
					sum += w * float64(r.buf[int(j-r.base)*ch+c])
				}

				r.out = append(r.out, float32(sum))
			}
		}

		r.nextOut++
	}

	if endOfInput {
		out := r.out
		r.Reset()
		return out, nil
	}

	// keep what the next output still needs
	next, _, _ := r.position(r.nextOut)
	keep := next - int64(r.half) + 1
	if drop := min(keep-r.base, int64(len(r.buf)/ch)); drop > 0 {
		// at a very low ratio the next output can be further on than what has been received: then everything buffered goes
		n := copy(r.buf, r.buf[int(drop)*ch:])
		r.buf = r.buf[:n]
		r.base += drop
	}

	return r.out, nil
}

// dot appends the weighted sum of the frames from offset (in samples) on for every channel
func (r *Resampler) dot(coefs []float64, offset int) {
	ch := r.channels
	buf := r.buf[offset : offset+len(coefs)*ch]

	switch ch {
	case 1:
		var s0 float64
		for k, w := range coefs {
			s0 += w * float64(buf[k])
		}
		r.out = append(r.out, float32(s0))
	case 2:
		var s0, s1 float64
		for k, w := range coefs {
			s0 += w * float64(buf[2*k])
			s1 += w * float64(buf[2*k+1])
		}
		r.out = append(r.out, float32(s0), float32(s1))
	default:
		for c := 0; c < ch; c++ {
			var sum float64
			for k, w := range coefs {
				sum += w * float64(buf[k*ch+c])
			}
			r.out = append(r.out, float32(sum))
		}
	}
}

// rightTaps is the number of frames after the one at the integer position that an output needs
func (r *Resampler) rightTaps() int {
	switch r.quality {
	case ZeroOrderHold:
		return 0
	}
	return r.half
}

// fillCoefs makes the weights for the 2*half input frames i0-half+1 ... i0+half, for a position frac after frame i0
func (r *Resampler) fillCoefs(c []float64, frac float64) {
	h := r.half

	switch r.quality {
	case ZeroOrderHold:
		c[0], c[1] = 1, 0
		return
	case Linear:
		c[0], c[1] = 1-frac, frac
		return
	}

	// index in the table: a position u (in zero crossings) is at u * perCrossing
	inc := r.scale * float64(r.d.perCrossing)
	last := float64(len(r.table) - 2)

	// frames at or before i0, that are m = 0, 1, ... frames and frac before the position
	for m := 0; m < h; m++ {
		c[h-1-m] = r.interp((frac+float64(m))*inc, last)
	}
	// frames after i0
	for m := 0; m < h; m++ {
		c[h+m] = r.interp((1-frac+float64(m))*inc, last)
	}

	for i := range c {
		c[i] *= r.scale
	}
}

func (r *Resampler) interp(x, last float64) float64 {
	if x >= last {
		return 0
	}
	i := int(x)
	f := x - float64(i)
	return r.table[i] + f*(r.table[i+1]-r.table[i])
}

var (
	tableMu    sync.Mutex
	tableCache = map[design][]float64{}
)

// cachedSincTable shares the tables, they are big and never change
func cachedSincTable(d design) []float64 {
	tableMu.Lock()
	defer tableMu.Unlock()

	t, ok := tableCache[d]
	if !ok {
		t = sincTable(d)
		tableCache[d] = t
	}

	return t
}

// sincTable is sinc(u) * kaiser(u / zeroCrossings) for u from 0 to zeroCrossings, perCrossing entries per unit
func sincTable(d design) []float64 {
	n := d.zeroCrossings*d.perCrossing + 2
	t := make([]float64, n)
	i0beta := besselI0(d.beta)

	for i := range t {
		u := float64(i) / float64(d.perCrossing)
		z := u / float64(d.zeroCrossings)
		if z >= 1 {
			continue
		}

		s := 1.0
		if u != 0 {
			s = math.Sin(math.Pi*u) / (math.Pi * u)
		}

		t[i] = s * besselI0(d.beta*math.Sqrt(1-z*z)) / i0beta
	}

	return t
}

// besselI0 is the modified Bessel function of the first kind, order 0
func besselI0(x float64) float64 {
	sum, term := 1.0, 1.0
	for k := 1; k < 200; k++ {
		term *= (x / (2 * float64(k))) * (x / (2 * float64(k)))
		sum += term
		if term < sum*1e-17 {
			break
		}
	}
	return sum
}
