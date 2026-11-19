package sndfile

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/hajimehoshi/go-mp3"
	"github.com/jfreymuth/oggvorbis"
	"github.com/mewkiz/flac"

	"github.com/almerlucke/sndfile/float"
)

// Info describes a sound file that is open for reading
type Info struct {
	Channels   int
	SampleRate float64
	// Frames is the length of the file in frames (a frame has a sample of every channel)
	Frames int64
}

// Reader streams the frames of a sound file from disk, for files that are too big to load, or that are read from the middle on (an instrument that
// plays from disk, a loop). A Reader is not safe for use from more than one goroutine.
//
// It is the small part of libsndfile that a streamer needs, so that a streamer does not need libsndfile itself: Open reads wav, aiff, mp3, flac and
// ogg vorbis in pure Go, and all the other formats with the libsndfile build tag.
type Reader[T float.Float] interface {
	// Info is the number of channels, the sample rate and the length of the file
	Info() Info
	// SeekFrame moves to a frame, counted from the start of the file. frame may be Info().Frames (the end), but not further. Seeking lossy files
	// (mp3, ogg) is exact to the frame
	SeekFrame(frame int64) error
	// ReadFrames fills buf with interleaved samples, as many whole frames as fit in it and the file has left, and gives the number of frames. At the
	// end of the file it gives 0 and no error, like libsndfile does
	ReadFrames(buf []T) (int64, error)
	// Close closes the file
	Close() error
}

// Open opens a sound file for streaming. The pure-Go readers handle wav, aiff, mp3, flac and ogg vorbis; other formats need the libsndfile build tag
func Open[T float.Float](filePath string) (Reader[T], error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}

	var head [12]byte
	n, _ := io.ReadFull(f, head[:])
	data := head[:n]

	var r Reader[T]

	switch {
	case isWAV(data):
		r, err = newPCMReader[T](f, parseWAV)
	case isAIFF(data):
		r, err = newPCMReader[T](f, parseAIFF)
	case len(data) >= 4 && string(data[0:4]) == "fLaC":
		r, err = newFLACReader[T](f)
	case len(data) >= 4 && string(data[0:4]) == "OggS":
		r, err = newOggReader[T](f)
	case isMP3(data):
		r, err = newMP3Reader[T](f)
	default:
		err = errNoDecoder
	}

	if errors.Is(err, errNoDecoder) {
		// no pure-Go reader for it: libsndfile, if it is built in
		_ = f.Close()
		return openLibsndfile[T](filePath)
	}

	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("open %s: %w", filePath, err)
	}

	return r, nil
}

func checkSeek(frame, frames int64) error {
	if frame < 0 || frame > frames {
		return fmt.Errorf("sndfile: seek to frame %d outside the file (%d frames)", frame, frames)
	}

	return nil
}

// pcmReader streams wav and aiff: the sample bytes are read from the file where they are wanted
type pcmReader[T float.Float] struct {
	f       *os.File
	pcm     pcmFile
	frames  int64
	pos     int64
	scratch []byte
}

func newPCMReader[T float.Float](f *os.File, parse func(io.ReaderAt, int64) (pcmFile, error)) (*pcmReader[T], error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}

	pcm, err := parse(f, st.Size())
	if err != nil {
		return nil, err
	}

	return &pcmReader[T]{f: f, pcm: pcm, frames: pcm.frames()}, nil
}

func (r *pcmReader[T]) Info() Info {
	return Info{Channels: r.pcm.layout.numChannels, SampleRate: float64(r.pcm.layout.sampleRate), Frames: r.frames}
}

func (r *pcmReader[T]) SeekFrame(frame int64) error {
	if err := checkSeek(frame, r.frames); err != nil {
		return err
	}

	r.pos = frame

	return nil
}

func (r *pcmReader[T]) ReadFrames(buf []T) (int64, error) {
	l := r.pcm.layout
	n := min(int64(len(buf)/l.numChannels), r.frames-r.pos)
	if n <= 0 {
		return 0, nil
	}

	size := int(n) * l.frameSize()
	if cap(r.scratch) < size {
		r.scratch = make([]byte, size)
	}
	src := r.scratch[:size]

	if _, err := r.f.ReadAt(src, r.pcm.dataOff+r.pos*int64(l.frameSize())); err != nil {
		// the file is shorter than the header said: give what there is
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			return 0, err
		}
		r.frames = r.pos
		return 0, nil
	}

	for c := 0; c < l.numChannels; c++ {
		convertChannel(buf[c:], l.numChannels, src, l, c, int(n))
	}

	r.pos += n

	return n, nil
}

func (r *pcmReader[T]) Close() error { return r.f.Close() }

const (
	// mp3PreRoll is how far before a frame a seek lands, in frames: more than the frames the state of the decoder depends on
	mp3PreRoll = 8192
	// mp3FrameAlign is a multiple of the frame length of both mpeg 1 (1152) and mpeg 2 (576)
	mp3FrameAlign = 1152
)

// mp3Reader streams mp3 with go-mp3, that makes stereo of everything and is seekable to the frame
type mp3Reader[T float.Float] struct {
	f       *os.File
	dec     *mp3.Decoder
	frames  int64
	pos     int64
	scratch []byte
	skipBuf [4 * 4096]byte
}

func newMP3Reader[T float.Float](f *os.File) (*mp3Reader[T], error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	dec, err := mp3.NewDecoder(f)
	if err != nil {
		return nil, err
	}

	return &mp3Reader[T]{f: f, dec: dec, frames: dec.Length() / 4}, nil
}

func (r *mp3Reader[T]) Info() Info {
	return Info{Channels: 2, SampleRate: float64(r.dec.SampleRate()), Frames: r.frames}
}

func (r *mp3Reader[T]) SeekFrame(frame int64) error {
	if err := checkSeek(frame, r.frames); err != nil {
		return err
	}

	// go-mp3 cannot seek to the end itself, there is no frame there: reads there give nothing anyway
	if frame >= r.frames {
		r.pos = frame
		return nil
	}

	// A frame of mp3 needs the frames before it (the bit reservoir, and the overlap of the filter bank): go-mp3 seeks to the frame itself and
	// gets those wrong. So land some frames before it and decode on, the state is the same after a few frames
	start := max(0, (frame-mp3PreRoll)/mp3FrameAlign*mp3FrameAlign)
	if _, err := r.dec.Seek(start*4, io.SeekStart); err != nil {
		return err
	}

	r.pos = start

	for r.pos < frame {
		n := min(frame-r.pos, int64(len(r.skipBuf)/4))
		got, err := io.ReadFull(r.dec, r.skipBuf[:n*4])
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return err
		}
		if got == 0 {
			break
		}
		r.pos += int64(got / 4)
	}

	return nil
}

func (r *mp3Reader[T]) ReadFrames(buf []T) (int64, error) {
	n := min(int64(len(buf)/2), r.frames-r.pos)
	if n <= 0 {
		return 0, nil
	}

	if cap(r.scratch) < int(n)*4 {
		r.scratch = make([]byte, int(n)*4)
	}
	src := r.scratch[:int(n)*4]

	got, err := io.ReadFull(r.dec, src)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return 0, err
	}

	frames := got / 4
	for i := 0; i < frames*2; i++ {
		buf[i] = T(int16(uint16(src[2*i])|uint16(src[2*i+1])<<8)) / 32768
	}

	r.pos += int64(frames)
	if frames == 0 {
		r.frames = r.pos
	}

	return int64(frames), nil
}

func (r *mp3Reader[T]) Close() error { return r.f.Close() }

// oggReader streams ogg vorbis
type oggReader[T float.Float] struct {
	f       *os.File
	r       *oggvorbis.Reader
	frames  int64
	pos     int64
	scratch []float32
}

func newOggReader[T float.Float](f *os.File) (*oggReader[T], error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	r, err := oggvorbis.NewReader(f)
	if err != nil {
		return nil, err
	}

	if r.Channels() < 1 {
		return nil, fmt.Errorf("sndfile: invalid ogg channel count %d", r.Channels())
	}

	return &oggReader[T]{f: f, r: r, frames: r.Length()}, nil
}

func (r *oggReader[T]) Info() Info {
	return Info{Channels: r.r.Channels(), SampleRate: float64(r.r.SampleRate()), Frames: r.frames}
}

func (r *oggReader[T]) SeekFrame(frame int64) error {
	if err := checkSeek(frame, r.frames); err != nil {
		return err
	}

	if err := r.r.SetPosition(frame); err != nil {
		return err
	}

	r.pos = frame

	return nil
}

func (r *oggReader[T]) ReadFrames(buf []T) (int64, error) {
	ch := r.r.Channels()
	n := min(int64(len(buf)/ch), r.frames-r.pos)
	if n <= 0 {
		return 0, nil
	}

	if cap(r.scratch) < int(n)*ch {
		r.scratch = make([]float32, int(n)*ch)
	}
	src := r.scratch[:int(n)*ch]

	got := 0
	for got < len(src) {
		k, err := r.r.Read(src[got:])
		got += k
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
	}

	frames := got / ch
	for i := 0; i < frames*ch; i++ {
		buf[i] = T(src[i])
	}

	r.pos += int64(frames)
	if frames == 0 {
		r.frames = r.pos
	}

	return int64(frames), nil
}

func (r *oggReader[T]) Close() error { return r.f.Close() }

// flacReader streams flac: whole flac frames are decoded, and the part of one that is read is kept
type flacReader[T float.Float] struct {
	f      *os.File
	stream *flac.Stream
	info   Info
	scale  T
	pos    int64
	// the samples of the frame that is being read, per channel, and how far they are read
	cur  [][]int32
	at   int
	skip int // samples to leave out of the next frame that is parsed, after a seek that landed before the frame
}

func newFLACReader[T float.Float](f *os.File) (*flacReader[T], error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	stream, err := flac.NewSeek(f)
	if err != nil {
		return nil, err
	}

	if stream.Info.NChannels < 1 || stream.Info.BitsPerSample < 1 {
		return nil, errors.New("sndfile: invalid flac stream info")
	}

	return &flacReader[T]{
		f:      f,
		stream: stream,
		info:   Info{Channels: int(stream.Info.NChannels), SampleRate: float64(stream.Info.SampleRate), Frames: int64(stream.Info.NSamples)},
		scale:  1 / T(int64(1)<<(stream.Info.BitsPerSample-1)),
	}, nil
}

func (r *flacReader[T]) Info() Info { return r.info }

func (r *flacReader[T]) SeekFrame(frame int64) error {
	if err := checkSeek(frame, r.info.Frames); err != nil {
		return err
	}

	r.cur, r.at, r.skip, r.pos = nil, 0, 0, frame
	if frame == r.info.Frames {
		return nil
	}

	landed, err := r.stream.Seek(uint64(frame))
	if err != nil {
		// the last frame of a stream with a fixed block size is shorter, and flac does not find it: seek to the frame before it and read on
		landed, err = r.stream.Seek(uint64(max(0, frame-int64(r.stream.Info.BlockSizeMax))))
		if err != nil {
			return err
		}
	}

	// the stream is at the start of the frame that has the sample: read on to the sample
	r.skip = int(frame - int64(landed))

	return nil
}

// fill makes sure that there are samples in cur, false at the end of the stream
func (r *flacReader[T]) fill() (bool, error) {
	for r.cur == nil || r.at >= len(r.cur[0]) {
		fr, err := r.stream.ParseNext()
		if err == io.EOF {
			return false, nil
		}
		if err != nil {
			return false, err
		}

		r.cur = make([][]int32, len(fr.Subframes))
		for c, sub := range fr.Subframes {
			r.cur[c] = sub.Samples
		}
		r.at = min(r.skip, len(r.cur[0]))
		r.skip = max(0, r.skip-len(r.cur[0]))
	}

	return true, nil
}

func (r *flacReader[T]) ReadFrames(buf []T) (int64, error) {
	ch := r.info.Channels
	want := min(int64(len(buf)/ch), r.info.Frames-r.pos)
	var done int64

	for done < want {
		ok, err := r.fill()
		if err != nil {
			return done, err
		}
		if !ok {
			r.info.Frames = r.pos // the file is shorter than it said
			break
		}

		n := min(int(want-done), len(r.cur[0])-r.at)
		for i := 0; i < n; i++ {
			for c := 0; c < ch; c++ {
				buf[(int(done)+i)*ch+c] = T(r.cur[c][r.at+i]) * r.scale
			}
		}

		r.at += n
		done += int64(n)
		r.pos += int64(n)
	}

	return done, nil
}

func (r *flacReader[T]) Close() error {
	_ = r.stream.Close()
	return r.f.Close()
}
