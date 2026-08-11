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

func (z ZeroCrossings) NearestPos(pos float64, direction int) ZeroCrossing {
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
		return z[closestIndex]
	}

	return ZeroCrossing{}
}

func (z ZeroCrossings) NearestPosFrames(pos int64, direction int) ZeroCrossing {
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
		return z[closestIndex]
	}

	return ZeroCrossing{}
}

func calculateZeroCrossings[T float.Float](buffer []T) ZeroCrossings {
	var (
		prevVal       T
		zeroCrossings ZeroCrossings
		n             = float64(len(buffer))
	)

	for i, v := range buffer {
		if prevVal < 0.0 && v > 0.0 {
			zeroCrossings = append(zeroCrossings, ZeroCrossing{
				PositionFrames: int64(i),
				Position:       float64(i) / n,
				Direction:      DirectionUp,
			})
		} else if prevVal > 0.0 && v < 0.0 {
			zeroCrossings = append(zeroCrossings, ZeroCrossing{
				PositionFrames: int64(i),
				Position:       float64(i) / n,
				Direction:      DirectionDown,
			})
		}
		prevVal = v
	}

	return zeroCrossings
}
