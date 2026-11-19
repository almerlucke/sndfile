package sndfile

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestMipMapFingerprint(t *testing.T) {
	a := mipMapFingerprint[float64](44100, 4)
	if a != mipMapFingerprint[float64](44100, 4) {
		t.Fatal("fingerprint is not deterministic")
	}
	// the cutoffs are relative to the sample rate, so the filters (and the fingerprint) are the same for every rate
	if a != mipMapFingerprint[float64](48000, 4) {
		t.Fatal("fingerprint depends on the sample rate while the filters do not")
	}
	if a == mipMapFingerprint[float64](44100, 5) {
		t.Fatal("fingerprint ignores the depth")
	}
	if a == mipMapFingerprint[float32](44100, 4) {
		t.Fatal("fingerprint ignores the sample type")
	}
}

func TestMipMapCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	old := MipMapCache
	MipMapCache = filepath.Join(dir, "cache")
	defer func() { MipMapCache = old }()

	// a small wav written by hand is not needed: use a generated sine as raw float data through the cache API
	frames := int64(4096)
	buf := make([]float64, frames)
	for i := range buf {
		buf[i] = math.Sin(2 * math.Pi * 440 * float64(i) / 44100)
	}
	mm, err := NewMipMap(buf, 44100, 3)
	if err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(dir, "x.wav")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := mipMapCachePath[float64](src, 44100, frames, 1, 3)
	if path == "" {
		t.Fatal("no cache path")
	}
	writeMipMapCache(path, frames, 1, 3, []*MipMap[float64]{mm})

	levels, ok := readMipMapCache[float64](path, frames, 1, 3)
	if !ok {
		t.Fatal("cache not read back")
	}
	for d := 1; d < 3; d++ {
		for i, v := range levels[0][d-1] {
			if v != mm.buffers[d][i] {
				t.Fatalf("level %d differs at %d", d, i)
			}
		}
	}

	// another depth means other levels, so another file
	if path == mipMapCachePath[float64](src, 44100, frames, 1, 4) {
		t.Fatal("cache path ignores the depth")
	}
}
