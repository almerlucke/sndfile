# sndfile

Read and write sound files in Go, without cgo.

```
go get github.com/almerlucke/sndfile
```

## Formats

| Format | Reading | Writing |
|---|---|---|
| WAV: PCM 8, 16, 24 and 32 bit, float32 and float64, WAVE_FORMAT_EXTENSIBLE | yes | float32 |
| AIFF and AIFC: PCM 8 to 32 bit, `sowt`, `fl32`, `fl64` | yes | AIFC `fl32` |
| FLAC | yes | no |
| Ogg Vorbis | yes | no |
| MP3 (MPEG 1 and 2) | yes | no |

A-law, µ-law, ADPCM, RF64 and the like are not decoded by the Go code, they need the libsndfile build below. The decoders are written in Go (WAV and AIFF are in this package, the others use
[go-mp3](https://github.com/hajimehoshi/go-mp3), [mewkiz/flac](https://github.com/mewkiz/flac)
and [oggvorbis](https://github.com/jfreymuth/oggvorbis)), so `CGO_ENABLED=0` works.

MP3 files are cut to their sound: the encoder delay and padding from the LAME (or Lavc, Lavf) tag are removed, so a gapless file loops without a click. An MP3 without such a tag is
left as it is. Mono MP3 files come out as two identical channels, and MPEG 2.5 (8, 11.025 and 12 kHz) is not supported by the pure Go decoder.

### libsndfile (optional)

Build with `-tags libsndfile` to use [libsndfile](https://libsndfile.github.io/libsndfile/) for everything the Go decoders can't open (needs cgo and the
libsndfile development files). Without the tag, such a file gives an `unsupported format` error that says so.

```
go build -tags libsndfile ./...
```

## Reading

Load a whole file into memory, one buffer per channel:

```go
sf, err := sndfile.NewSoundFile[float32]("kick.wav")
if err != nil {
	return err
}

fmt.Println(sf.NumChannels(), sf.SampleRate(), sf.NumFrames(), sf.Duration())

left := sf.Buffer(0, 0)
v := sf.Lookup(123.5, 0, 0, false) // interpolated lookup at a position in frames
```

`NewMipMapSoundFile` also builds band-limited copies of the file for alias-free playback at other speeds (`LookupWithSpeed`), and `ZeroCrossings`
finds the zero crossings of a channel.

Or stream it from disk, for files that are too large or too long to load:

```go
r, err := sndfile.Open[float32]("long.flac")
if err != nil {
	return err
}
defer r.Close()

info := r.Info() // Channels, SampleRate, Frames

if err := r.SeekFrame(info.Frames / 2); err != nil { // exact, also for mp3 and ogg
	return err
}

buf := make([]float32, 4096*info.Channels) // interleaved
for {
	n, err := r.ReadFrames(buf)
	if err != nil {
		return err
	}
	if n == 0 { // end of file
		break
	}
	process(buf[:n*int64(info.Channels)])
}
```

The sample type is `float32` or `float64`.

## Writing

```go
wr, err := writer.NewWithOptions("out.wav", writer.WAV, 2, 48000, writer.Options{
	ConvertSampleRate: true,
	InputSampleRate:   44100,
	SrConvQuality:     resample.SincBest,
	Normalize:         true,
})
if err != nil {
	return err
}

if err := wr.Write(samples, true); err != nil { // interleaved, true marks the last block
	return err
}

return wr.Close()
```

Files are written as 32-bit float. The sizes in the header are refreshed about every 1 MiB, so a file that was not closed (a crash) still plays up to
that point. `Normalize` scales to the absolute peak after sample rate conversion.

## Resampling

`dsp/resample` is a Kaiser-windowed sinc resampler with the quality levels of libsamplerate (`SincBest`, `SincMedium`, `SincFastest`, `ZeroOrderHold`,
`Linear`). The package documentation compares its accuracy and speed with libsamplerate: it is a few dB (SincBest) to about 15 dB (SincMedium, SincFastest) more accurate, and 1.5 to 2 times faster for the usual
fractions such as 44100 to 48000 (about 2 times slower for irrational ratios).

```go
rs, err := resample.New(resample.SincBest, 2)
if err != nil {
	return err
}
rs.SetRatio(48000.0 / 44100.0)
out, err := rs.Process(in, false) // in and out are interleaved; true for the last block
```

## Other packages

- `dsp/filters`: windowed-sinc low-pass and high-pass filter coefficients
- `dsp/windows`: window functions (Blackman, Hamming, Nuttall, ...)

## Testing

```
go test ./...                       # pure Go
go test -tags libsndfile ./...      # also the libsndfile fallback
```

`testdata/gen.sh` regenerates the fixture files (needs ffmpeg), each with a `.f32` reference of the expected samples.

## License

MIT, see [LICENSE](LICENSE).
