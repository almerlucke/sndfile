package sndfile

import "github.com/almerlucke/sndfile/float"

type LookupParam[T float.Float] struct {
	Index1   int64
	Index2   int64
	Fraction float64
}

func NewLookupParam[T float.Float](pos float64, n int64, wrap bool) *LookupParam[T] {
	i1 := int64(pos)
	i2 := i1 + 1

	if wrap {
		i2 = i2 % n
	} else if i2 >= n {
		i2 = n - 1
	}

	return &LookupParam[T]{
		Index1:   i1,
		Index2:   i2,
		Fraction: pos - float64(i1),
	}
}

func (lp *LookupParam[T]) Lookup(b []T) T {
	s1 := b[lp.Index1]
	return s1 + T(lp.Fraction)*(b[lp.Index2]-s1)
}
