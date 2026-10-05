package sndfile

import (
	"math"
	"math/rand/v2"
	"testing"
)

// linearNearest is the straightforward search the binary search must agree with
func linearNearest(z ZeroCrossings, dist func(ZeroCrossing) float64, direction int) (ZeroCrossing, bool) {
	best, bestDist := -1, math.MaxFloat64
	for i, c := range z {
		if direction == DirectionAny || c.Direction == direction {
			if d := dist(c); d < bestDist {
				best, bestDist = i, d
			}
		}
	}
	if best < 0 {
		return ZeroCrossing{}, false
	}
	return z[best], true
}

func TestNearestMatchesLinearSearch(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))

	for trial := range 200 {
		// a random signal gives a realistic set of crossings
		buf := make([]float32, 50+r.IntN(2000))
		for i := range buf {
			buf[i] = float32(math.Sin(float64(i)*r.Float64()*0.5) + (r.Float64()-0.5)*0.3)
		}
		z := calculateZeroCrossings(buf)

		for range 50 {
			pos := r.Float64()*1.2 - 0.1 // also before the start and past the end
			frames := int64(pos * float64(len(buf)))
			for _, dir := range []int{DirectionAny, DirectionUp, DirectionDown} {
				got, gotOK := z.NearestPos(pos, dir)
				want, wantOK := linearNearest(z, func(c ZeroCrossing) float64 { return math.Abs(c.Position - pos) }, dir)
				if got != want || gotOK != wantOK {
					t.Fatalf("trial %d: NearestPos(%v, %d) = %+v, linear search %+v", trial, pos, dir, got, want)
				}

				gotF, okF := z.NearestPosFrames(frames, dir)
				wantF, wantOKF := linearNearest(z, func(c ZeroCrossing) float64 { return math.Abs(float64(c.PositionFrames - frames)) }, dir)
				if gotF != wantF || okF != wantOKF {
					t.Fatalf("trial %d: NearestPosFrames(%v, %d) = %+v, linear search %+v", trial, frames, dir, gotF, wantF)
				}
			}
		}
	}

	if _, ok := ZeroCrossings(nil).NearestPos(0.5, DirectionAny); ok {
		t.Fatal("no crossings should find nothing")
	}
}
