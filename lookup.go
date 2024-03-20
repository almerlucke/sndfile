package sndfile

type LookupParam struct {
	Index1   int64
	Index2   int64
	Fraction float64
}

func NewLookupParam(pos float64, n int64, wrap bool) *LookupParam {
	i1 := int64(pos)
	i2 := i1 + 1

	if wrap {
		i2 = i2 % n
	} else if i2 >= n {
		i2 = n - 1
	}

	return &LookupParam{
		Index1:   i1,
		Index2:   i2,
		Fraction: pos - float64(i1),
	}
}

func (lp *LookupParam) Lookup(b []float64) float64 {
	s1 := b[lp.Index1]

	return s1 + lp.Fraction*(b[lp.Index2]-s1)
}
