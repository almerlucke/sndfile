package sndfile

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/almerlucke/sndfile/float"
)

// MipMapCache is a folder where the mipmaps of the files are kept, so that a file is filtered once and read back after that: building the lower
// resolutions of a big instrument takes seconds to a minute, reading them takes a moment. "" is no cache. A file is kept by its path, size and
// time, and the settings of the filters: a file that changes is made again
var MipMapCache string

// mipMapVersion is the version of the layout of the cache file. A change in the filters needs no bump: the key holds a fingerprint of the filters
const mipMapVersion = 2

var mipMapMagic = [8]byte{'S', 'N', 'D', 'M', 'I', 'P', 0, mipMapVersion}

// mipMapCachePath is the file of the cache for a sound file at a depth, or "" if there is no cache or the file cannot be told by its stat
func mipMapCachePath[T float.Float](path string, sampleRate float64, frames int64, channels, depth int) string {
	if MipMapCache == "" || depth < 2 {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return ""
	}
	var zero T
	key := fmt.Sprintf("%s|%d|%d|%d|%d|%d|%d|%T|%x", abs, fi.Size(), fi.ModTime().UnixNano(), frames, channels, depth, mipMapVersion, zero,
		mipMapFingerprint[T](sampleRate, depth))
	sum := sha256.Sum256([]byte(key))
	name := strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
	return filepath.Join(MipMapCache, fmt.Sprintf("%s-%s.mip", sanitize(name), hex.EncodeToString(sum[:8])))
}

// mipMapFingerprint identifies the filters that make levels 1 to depth-1 for a sample rate: the kernel of every level, and what the filter does with
// a fixed probe signal, so that a change in the kernel (taps, window, cutoffs) as well as a change in the convolution itself gives another key
func mipMapFingerprint[T float.Float](sampleRate float64, depth int) [8]byte {
	probe := make([]T, 1024)
	seed := uint32(1)
	for i := range probe {
		seed = seed*1664525 + 1013904223
		probe[i] = T(float64(seed>>8)/float64(1<<23) - 1)
	}
	h := sha256.New()
	for d := 1; d < depth; d++ {
		fir := mipMapFilter[T](sampleRate, d)
		_ = binary.Write(h, binary.LittleEndian, fir.Sinc.LowPassCoefs())
		out, err := fir.LowPass(probe)
		if err != nil {
			// no level can be made with this filter: the key only has to differ from the keys of working filters
			h.Write([]byte(err.Error()))
			continue
		}
		_ = binary.Write(h, binary.LittleEndian, out)
	}
	var fp [8]byte
	copy(fp[:], h.Sum(nil))
	return fp
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return '_'
		}
		return r
	}, s)
}

// readMipMapCache gives the levels 1 to depth-1 of every channel: [channel][level-1], or false if they are not kept (or not whole)
func readMipMapCache[T float.Float](path string, frames int64, channels, depth int) ([][][]T, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	var magic [8]byte
	if _, err := io.ReadFull(f, magic[:]); err != nil || magic != mipMapMagic {
		return nil, false
	}
	var head [3]uint64
	if err := binary.Read(f, binary.LittleEndian, &head); err != nil || head[0] != uint64(channels) || head[1] != uint64(depth) || head[2] != uint64(frames) {
		return nil, false
	}
	levels := make([][][]T, channels)
	for c := range levels {
		levels[c] = make([][]T, depth-1)
		for d := range levels[c] {
			buf := make([]T, frames)
			var err error
			switch b := any(buf).(type) {
			case []float32:
				err = binary.Read(f, binary.LittleEndian, b)
			case []float64:
				err = binary.Read(f, binary.LittleEndian, b)
			default:
				return nil, false
			}
			if err != nil {
				return nil, false
			}
			levels[c][d] = buf
		}
	}
	// nothing may follow: a file that is longer than it says is not ours
	var extra [1]byte
	if n, _ := f.Read(extra[:]); n != 0 {
		return nil, false
	}
	return levels, true
}

// writeMipMapCache keeps the levels, whole or not at all (written aside and renamed). Failing to keep them is not a failure of the sound
func writeMipMapCache[T float.Float](path string, frames int64, channels, depth int, mms []*MipMap[T]) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mip-*")
	if err != nil {
		return
	}
	ok := false
	defer func() {
		tmp.Close()
		if !ok {
			os.Remove(tmp.Name())
		}
	}()
	w := newBufferedWriter(tmp)
	if _, err := w.Write(mipMapMagic[:]); err != nil {
		return
	}
	if err := binary.Write(w, binary.LittleEndian, [3]uint64{uint64(channels), uint64(depth), uint64(frames)}); err != nil {
		return
	}
	for _, mm := range mms {
		for d := 1; d < depth; d++ {
			var err error
			switch b := any(mm.buffers[d]).(type) {
			case []float32:
				err = binary.Write(w, binary.LittleEndian, b)
			case []float64:
				err = binary.Write(w, binary.LittleEndian, b)
			default:
				return
			}
			if err != nil {
				return
			}
		}
	}
	if err := w.Flush(); err != nil {
		return
	}
	if err := tmp.Close(); err != nil {
		return
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return
	}
	ok = true
}

// newBufferedWriter is a writer with a big buffer: the levels are written in large pieces
func newBufferedWriter(w io.Writer) *bufWriter { return &bufWriter{w: w, buf: make([]byte, 0, 1<<20)} }

type bufWriter struct {
	w   io.Writer
	buf []byte
}

func (b *bufWriter) Write(p []byte) (int, error) {
	if len(b.buf)+len(p) > cap(b.buf) {
		if err := b.Flush(); err != nil {
			return 0, err
		}
		if len(p) > cap(b.buf) {
			return b.w.Write(p)
		}
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *bufWriter) Flush() error {
	if len(b.buf) == 0 {
		return nil
	}
	_, err := b.w.Write(b.buf)
	b.buf = b.buf[:0]
	return err
}
