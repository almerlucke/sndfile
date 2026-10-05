package sndfile

import (
	"math"
	"sort"

	"github.com/almerlucke/sndfile/float"
)

const (
	DirectionAny  = 0
	DirectionUp   = 1
	DirectionDown = -1
)

type ZeroCrossing struct {
	PositionFrames int64
	Position       float64
	Direction      int
}

type ZeroCrossings []ZeroCrossing

// NearestPos returns the zero crossing closest to pos (0-1), optionally only crossings in the given
// direction. Zero crossings are sorted by position, so it uses a binary search: fast also for long
// files with many crossings (granular sources call it for every grain)
func (z ZeroCrossings) NearestPos(pos float64, direction int) (ZeroCrossing, bool) {
	start := sort.Search(len(z), func(i int) bool { return z[i].Position >= pos })
	index := z.nearest(start, direction, func(c ZeroCrossing) float64 { return math.Abs(c.Position - pos) })
	if index < 0 {
		return ZeroCrossing{}, false
	}
	return z[index], true
}

// NearestPosFrames returns the zero crossing closest to the frame position, optionally only
// crossings in the given direction
func (z ZeroCrossings) NearestPosFrames(pos int64, direction int) (ZeroCrossing, bool) {
	start := sort.Search(len(z), func(i int) bool { return z[i].PositionFrames >= pos })
	index := z.nearest(start, direction, func(c ZeroCrossing) float64 {
		return math.Abs(float64(c.PositionFrames - pos))
	})
	if index < 0 {
		return ZeroCrossing{}, false
	}
	return z[index], true
}

// nearest walks outwards from the insertion point start to the closest crossing with a matching
// direction, on equal distance the earlier crossing wins
func (z ZeroCrossings) nearest(start int, direction int, dist func(ZeroCrossing) float64) int {
	matches := func(c ZeroCrossing) bool { return direction == DirectionAny || c.Direction == direction }

	left := start - 1
	for left >= 0 && !matches(z[left]) {
		left--
	}
	right := start
	for right < len(z) && !matches(z[right]) {
		right++
	}

	var best int
	switch {
	case left < 0 && right >= len(z):
		return -1
	case left < 0:
		best = right
	case right >= len(z):
		best = left
	case dist(z[right]) < dist(z[left]):
		best = right
	default:
		best = left
	}

	// crossings can share a frame position, then the earliest one wins
	for i := best - 1; i >= 0; i-- {
		if !matches(z[i]) {
			continue
		}
		if dist(z[i]) != dist(z[best]) {
			break
		}
		best = i
	}

	return best
}

func calculateZeroCrossings[T float.Float](buffer []T) ZeroCrossings {
	if len(buffer) < 2 {
		return nil
	}

	var (
		zeroCrossings  ZeroCrossings
		n              = float64(len(buffer))
		lastNonZero    T
		lastNonZeroIdx = -1
	)

	// Find the initial non-zero sample
	for i, v := range buffer {
		if v != 0.0 {
			lastNonZero = v
			lastNonZeroIdx = i
			break
		}
	}

	if lastNonZeroIdx == -1 {
		// All samples are 0.0
		return nil
	}

	for i := lastNonZeroIdx + 1; i < len(buffer); i++ {
		v := buffer[i]
		if v == 0.0 {
			continue
		}

		if (lastNonZero < 0.0 && v > 0.0) || (lastNonZero > 0.0 && v < 0.0) {
			// Sub-sample linear interpolation fraction between last non-zero and current sample
			fraction := float64(-lastNonZero) / float64(v-lastNonZero)
			exactFrame := float64(lastNonZeroIdx) + fraction*float64(i-lastNonZeroIdx)

			direction := DirectionUp
			if lastNonZero > 0.0 {
				direction = DirectionDown
			}

			zeroCrossings = append(zeroCrossings, ZeroCrossing{
				PositionFrames: int64(math.Round(exactFrame)),
				Position:       exactFrame / n,
				Direction:      direction,
			})
		}

		lastNonZero = v
		lastNonZeroIdx = i
	}

	return zeroCrossings
}
