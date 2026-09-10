package sndfile

import (
	"math"

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

func (z ZeroCrossings) NearestPos(pos float64, direction int) (ZeroCrossing, bool) {
	var (
		closestIndex = -1
		closestDist  = math.MaxFloat64
	)

	for i, c := range z {
		if direction == DirectionAny || c.Direction == direction {
			dist := math.Abs(c.Position - pos)
			if dist < closestDist {
				closestDist = dist
				closestIndex = i
			}
		}
	}

	if closestIndex > -1 {
		return z[closestIndex], true
	}

	return ZeroCrossing{}, false
}

func (z ZeroCrossings) NearestPosFrames(pos int64, direction int) (ZeroCrossing, bool) {
	var (
		closestIndex = -1
		closestDist  = int64(math.MaxInt64)
	)

	for i, c := range z {
		if direction == DirectionAny || c.Direction == direction {
			dist := c.PositionFrames - pos
			if dist < 0 {
				dist *= -1
			}
			if dist < closestDist {
				closestDist = dist
				closestIndex = i
			}
		}
	}

	if closestIndex > -1 {
		return z[closestIndex], true
	}

	return ZeroCrossing{}, false
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
