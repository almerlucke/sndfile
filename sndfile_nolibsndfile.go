//go:build !libsndfile

package sndfile

import (
	"fmt"

	"github.com/almerlucke/sndfile/float"
)

// haveLibsndfile is true in a build with libsndfile
const haveLibsndfile = false

// newSoundFileLibsndfile is the fallback for formats without a pure-Go decoder: without the libsndfile build tag there is none
func newSoundFileLibsndfile[T float.Float](filePath string) (*SoundFile[T], error) {
	return nil, fmt.Errorf("sndfile: unsupported format %q (supported: wav, aiff, mp3, flac, ogg vorbis; build with -tags libsndfile for more)", filePath)
}

// openLibsndfile is the fallback of Open for formats without a pure-Go reader: without the libsndfile build tag there is none
func openLibsndfile[T float.Float](filePath string) (Reader[T], error) {
	return nil, fmt.Errorf("sndfile: unsupported format %q (supported: wav, aiff, mp3, flac, ogg vorbis; build with -tags libsndfile for more)", filePath)
}
