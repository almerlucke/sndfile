package sndfile

import "github.com/almerlucke/sndfile/float"

type SoundBank[T float.Float] map[string]SoundFiler[T]
