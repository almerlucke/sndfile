package aifc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/almerlucke/sndfile/writer/backend"
	"github.com/almerlucke/sndfile/writer/backend/aifc/float80"
)

// headerRefreshBytes is how many bytes of samples are written before the sizes in the header are brought up to date again. The sizes are only right at
// Close, if the program stops before that a file would say it has no samples. Refreshing them now and then limits what a crash loses to what was
// written since: that is about 3 seconds of stereo at 44.1 kHz
var headerRefreshBytes int64 = 1 << 20

// maxSampleBytes is the most bytes of samples an aifc file can hold: the size of the FORM chunk, with the header (100 bytes, with room to spare) and
// the samples, is 32 bits
const maxSampleBytes = math.MaxUint32 - 100

const (
	aifcVersion1        = uint32(0xA2805140)
	aifcCompressionName = "32-bit floating point"
	aifcCompressionType = "fl32"
)

func toPascalBytes(str string) []byte {
	strlen := len(str)
	size := strlen + 1
	ps := make([]byte, size+size%2) // pad 1 byte if uneven
	ps[0] = byte(strlen)
	for i, c := range str {
		ps[i+1] = byte(c)
	}
	return ps
}

type AIFC struct {
	numChannels     int16
	numSampleFrames uint32
	sampleRate      float64
	file            *os.File
	// sinceRefresh is the number of bytes written since the sizes in the header were written
	sinceRefresh int64
}

func New(filePath string, numChannels int, sampleRate float64) (*AIFC, error) {
	if numChannels < 1 || numChannels > math.MaxInt16 {
		return nil, fmt.Errorf("aifc: %d channels, an aifc file has 1 to %d", numChannels, math.MaxInt16)
	}

	if !(sampleRate > 0) || math.IsInf(sampleRate, 0) {
		return nil, fmt.Errorf("aifc: sample rate %v out of range", sampleRate)
	}

	file, err := os.Create(filePath)
	if err != nil {
		return nil, err
	}

	aifc := &AIFC{
		numChannels: int16(numChannels),
		sampleRate:  sampleRate,
		file:        file,
	}

	err = aifc.writeHeader()
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	return aifc, nil
}

func (aifc *AIFC) Close() error {
	var errs []error
	var err error

	err = aifc.updateSizes()
	if err != nil {
		errs = append(errs, err)
	}

	err = aifc.file.Close()
	if err != nil {
		errs = append(errs, err)
	}

	if len(errs) != 0 {
		return errors.Join(errs...)
	}

	return nil
}

func (aifc *AIFC) soundChunkSize() uint32 {
	return 8 + aifc.numSampleFrames*uint32(aifc.numChannels)*4
}

func (aifc *AIFC) commonChunkSize() uint32 {
	return 44
}

func (aifc *AIFC) versionChunkSize() uint32 {
	return 4
}

func (aifc *AIFC) formChunkSize() uint32 {
	return 4 + aifc.versionChunkSize() + aifc.commonChunkSize() + aifc.soundChunkSize() + 24
}

// updateSizes brings the sizes in the header up to date, and leaves the position in the file where it was
func (aifc *AIFC) updateSizes() error {
	end, err := aifc.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}

	if err := aifc.writeSizes(); err != nil {
		return err
	}

	aifc.sinceRefresh = 0

	_, err = aifc.file.Seek(end, io.SeekStart)

	return err
}

func (aifc *AIFC) writeSizes() error {
	// Seek form size
	_, err := aifc.file.Seek(4, io.SeekStart)
	if err != nil {
		return err
	}

	// Update form size
	size := aifc.formChunkSize()
	err = binary.Write(aifc.file, binary.BigEndian, size)
	if err != nil {
		return err
	}

	// Seek common number of frames
	_, err = aifc.file.Seek(34, io.SeekStart)
	if err != nil {
		return err
	}

	// Update number of frames
	err = binary.Write(aifc.file, binary.BigEndian, aifc.numSampleFrames)
	if err != nil {
		return err
	}

	// Seek ssnd size
	_, err = aifc.file.Seek(80, io.SeekStart)
	if err != nil {
		return err
	}

	// Update ssnd size
	err = binary.Write(aifc.file, binary.BigEndian, aifc.soundChunkSize())
	if err != nil {
		return err
	}

	return nil
}

func (aifc *AIFC) Normalize(max float32) error {
	var err error

	// Read and write buffers
	readerBuffer := make([]byte, 8192)
	writerBuffer := make([]byte, 0, 8192)

	// Seek to start of sound data
	_, err = aifc.file.Seek(92, io.SeekStart)
	if err != nil {
		return err
	}

	if max <= 0.0 {
		return nil
	}

	var (
		oom = 1.0 / max
		pos int64
		n   int
	)

	// Loop through all samples
	for {
		pos, err = aifc.file.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}

		// Read 8192 bytes if possible
		n, err = aifc.file.Read(readerBuffer)
		if err != nil {
			if err != io.EOF {
				return err
			}
			// Break if EOF
			break
		}

		// Create reader and writer objects for conversion
		byteReader := bytes.NewReader(readerBuffer[:n])
		writer := bytes.NewBuffer(writerBuffer[:0])

		// Normalize read bytes and convert back
		for {
			var f float32

			err = binary.Read(byteReader, binary.BigEndian, &f)
			if err != nil {
				if err != io.EOF {
					return err
				}
				break
			}

			err = binary.Write(writer, binary.BigEndian, f*oom)
			if err != nil {
				return err
			}
		}

		// Seek last pos
		_, err = aifc.file.Seek(pos, io.SeekStart)
		if err != nil {
			return err
		}

		// Overwrite with normalized samples
		_, err = aifc.file.Write(writer.Bytes())
		if err != nil {
			return err
		}
	}

	return nil
}

func (aifc *AIFC) writeCommon() error {
	// COMM
	_, err := aifc.file.Write([]byte("COMM"))
	if err != nil {
		return err
	}

	compressionName := toPascalBytes(aifcCompressionName)

	// Size
	err = binary.Write(aifc.file, binary.BigEndian, uint32(len(compressionName)+22))
	if err != nil {
		return err
	}

	// Num Channels
	err = binary.Write(aifc.file, binary.BigEndian, aifc.numChannels)
	if err != nil {
		return err
	}

	// Write template number of frames, fill in later
	err = binary.Write(aifc.file, binary.BigEndian, uint32(0))
	if err != nil {
		return err
	}

	// Sample size
	err = binary.Write(aifc.file, binary.BigEndian, int16(32))
	if err != nil {
		return err
	}

	// Sample rate
	sampleRateBytes := float80.NewFromFloat64(aifc.sampleRate).Bytes()
	_, err = aifc.file.Write(sampleRateBytes)
	if err != nil {
		return err
	}

	// Compression type
	_, err = aifc.file.Write([]byte(aifcCompressionType))
	if err != nil {
		return err
	}

	// Compression name
	_, err = aifc.file.Write(compressionName)
	if err != nil {
		return err
	}

	return nil
}

func (aifc *AIFC) writeVersion() error {
	// FVER
	_, err := aifc.file.Write([]byte("FVER"))
	if err != nil {
		return err
	}

	// Size
	err = binary.Write(aifc.file, binary.BigEndian, uint32(4))
	if err != nil {
		return err
	}

	// Version
	err = binary.Write(aifc.file, binary.BigEndian, aifcVersion1)
	if err != nil {
		return err
	}

	return nil
}

func (aifc *AIFC) writeDataHeader() error {
	// SSND
	_, err := aifc.file.Write([]byte("SSND"))
	if err != nil {
		return err
	}

	// Size template
	err = binary.Write(aifc.file, binary.BigEndian, uint32(0))
	if err != nil {
		return err
	}

	// Offset
	err = binary.Write(aifc.file, binary.BigEndian, uint32(0))
	if err != nil {
		return err
	}

	// Block allign
	err = binary.Write(aifc.file, binary.BigEndian, uint32(0))
	if err != nil {
		return err
	}

	return nil
}

func (aifc *AIFC) writeHeader() error {
	// FORM
	_, err := aifc.file.Write([]byte("FORM"))
	if err != nil {
		return err
	}

	// Write template size for now until we know full size
	err = binary.Write(aifc.file, binary.BigEndian, int32(0))
	if err != nil {
		return err
	}

	// AIFC
	_, err = aifc.file.Write([]byte("AIFC")) // 4 bytes
	if err != nil {
		return err
	}

	// Version chunk
	err = aifc.writeVersion() // 12 bytes
	if err != nil {
		return err
	}

	// Common chunk
	err = aifc.writeCommon() // 22 + 22 = 44
	if err != nil {
		return err
	}

	// Data header
	err = aifc.writeDataHeader()
	if err != nil {
		return err
	}

	return nil
}

func (aifc *AIFC) Write(items []float32) error {
	if len(items)%int(aifc.numChannels) != 0 {
		return fmt.Errorf("aifc: %d samples are not whole frames of %d channels", len(items), aifc.numChannels)
	}

	numFrames := len(items) / int(aifc.numChannels)

	if (uint64(aifc.numSampleFrames)+uint64(numFrames))*uint64(aifc.numChannels)*4 > maxSampleBytes {
		return backend.ErrTooLarge
	}

	var buf bytes.Buffer

	for _, item := range items {
		_ = binary.Write(&buf, binary.BigEndian, item)
	}

	aifc.numSampleFrames += uint32(numFrames)

	if _, err := aifc.file.Write(buf.Bytes()); err != nil {
		return err
	}

	aifc.sinceRefresh += int64(buf.Len())
	if aifc.sinceRefresh >= headerRefreshBytes {
		return aifc.updateSizes()
	}

	return nil
}
