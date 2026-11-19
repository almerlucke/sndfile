//go:build libsndfile

package sndfile

import (
	"github.com/almerlucke/sndfile/float"
	"github.com/mkb218/gosndfile/sndfile"
)

// newSoundFileLibsndfile loads a sound file with libsndfile, for the formats that have no pure-Go decoder
func newSoundFileLibsndfile[T float.Float](filePath string) (*SoundFile[T], error) {
	info := sndfile.Info{}

	file, err := sndfile.Open(filePath, sndfile.Read, &info)
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = file.Close()
	}()

	// Create one big buffer to hold all samples
	fileBuffer := make([]T, int64(info.Channels)*info.Frames)

	// Create separate channels by splitting buffer into info.Channels parts
	channels := make([][]T, info.Channels)
	for i := int32(0); i < info.Channels; i++ {
		channels[i] = fileBuffer[int64(i)*info.Frames : int64(i+1)*info.Frames]
	}

	// Deinterleave in blocks
	sampleBlockSize := int64(2048) * int64(info.Channels)
	samples := make([]T, sampleBlockSize)
	frameIndex := int64(0)

	for {
		framesRead, err := file.ReadFrames(samples)
		if err != nil {
			return nil, err
		}

		if framesRead == 0 {
			break
		}

		for i := range framesRead {
			for j := int64(0); j < int64(info.Channels); j++ {
				channels[j][frameIndex+i] = samples[i*int64(info.Channels)+j]
			}
		}

		frameIndex += framesRead
	}

	// Find zero crossings
	zeroCrossings := make([]ZeroCrossings, info.Channels)
	for i := range zeroCrossings {
		zeroCrossings[i] = calculateZeroCrossings(channels[i])
	}

	sf := SoundFile[T]{}
	sf.duration = float64(info.Frames) / float64(info.Samplerate)
	sf.numFrames = info.Frames
	sf.channels = channels
	sf.zeroCrossings = zeroCrossings
	sf.sampleRate = float64(info.Samplerate)
	sf.out = make([]T, info.Channels)

	return &sf, nil
}
