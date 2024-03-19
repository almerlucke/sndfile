package wav

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
)

type Wav struct {
	numChannels  int16
	totalSamples uint32
	sampleRate   int32
	file         *os.File
}

func New(filePath string, numChannels int, sampleRate float64) (*Wav, error) {
	file, err := os.Create(filePath)
	if err != nil {
		return nil, err
	}

	wav := &Wav{
		numChannels: int16(numChannels),
		sampleRate:  int32(sampleRate),
		file:        file,
	}

	err = wav.writeHeader()
	if err != nil {
		_ = file.Close()
		return nil, err
	}

	return wav, nil
}

func (wav *Wav) Close() error {
	var errs []error
	var err error

	err = wav.updateSizes()
	if err != nil {
		errs = append(errs, err)
	}

	err = wav.file.Close()
	if err != nil {
		errs = append(errs, err)
	}

	if len(errs) != 0 {
		return errors.Join(errs...)
	}

	return nil
}

func (wav *Wav) writeHeader() error {
	var long int32
	var short int16

	_, err := wav.file.Write([]byte("RIFF")) // 0
	if err != nil {
		return nil
	}

	// write bogus total file size to be overwritten later
	err = binary.Write(wav.file, binary.LittleEndian, long) // 4
	if err != nil {
		return nil
	}

	_, err = wav.file.Write([]byte("WAVE")) // 8
	if err != nil {
		return nil
	}

	_, err = wav.file.Write([]byte("fmt ")) // 12
	if err != nil {
		return nil
	}

	long = 18                                               // size of fmt chunk
	err = binary.Write(wav.file, binary.LittleEndian, long) // 16
	if err != nil {
		return nil
	}

	short = 3                                                // float32 format
	err = binary.Write(wav.file, binary.LittleEndian, short) // 20
	if err != nil {
		return nil
	}

	short = wav.numChannels
	err = binary.Write(wav.file, binary.LittleEndian, short) // 22
	if err != nil {
		return nil
	}

	err = binary.Write(wav.file, binary.LittleEndian, wav.sampleRate) // 24
	if err != nil {
		return nil
	}

	long = (wav.sampleRate * 32 * int32(wav.numChannels)) / 8
	err = binary.Write(wav.file, binary.LittleEndian, long) // 28
	if err != nil {
		return nil
	}

	short = int16((32 * int32(wav.numChannels)) / 8)
	err = binary.Write(wav.file, binary.LittleEndian, short) // 32
	if err != nil {
		return nil
	}

	short = 32
	err = binary.Write(wav.file, binary.LittleEndian, short) // 34
	if err != nil {
		return nil
	}

	short = 0                                                // size of extension
	err = binary.Write(wav.file, binary.LittleEndian, short) // 36
	if err != nil {
		return nil
	}

	_, err = wav.file.Write([]byte("fact")) // 38
	if err != nil {
		return nil
	}

	long = 4
	err = binary.Write(wav.file, binary.LittleEndian, long) // 42
	if err != nil {
		return nil
	}

	long = 0                                                // num sample frames to be overwritten later
	err = binary.Write(wav.file, binary.LittleEndian, long) // 46
	if err != nil {
		return nil
	}

	_, err = wav.file.Write([]byte("data")) // 50
	if err != nil {
		return nil
	}

	long = 0                                                // data section size to be overwritten later
	err = binary.Write(wav.file, binary.LittleEndian, long) // 54
	if err != nil {
		return nil
	}

	return nil
}

func (wav *Wav) updateSizes() error {
	var size uint32

	// Seek total size
	_, err := wav.file.Seek(4, io.SeekStart)
	if err != nil {
		return err
	}

	// Update total size
	size = 50 + wav.totalSamples*4
	err = binary.Write(wav.file, binary.LittleEndian, size)
	if err != nil {
		return err
	}

	// Seek fact section size
	_, err = wav.file.Seek(46, io.SeekStart)
	if err != nil {
		return err
	}

	// Update fact section size
	size = wav.totalSamples / uint32(wav.numChannels)
	err = binary.Write(wav.file, binary.LittleEndian, size)
	if err != nil {
		return err
	}

	// Seek data section size
	_, err = wav.file.Seek(54, io.SeekStart)
	if err != nil {
		return err
	}

	// Update data section size
	size = wav.totalSamples * 4
	err = binary.Write(wav.file, binary.LittleEndian, size)
	if err != nil {
		return err
	}

	return nil
}

func (wav *Wav) Normalize(max float32) error {
	var err error

	// Read and write buffers
	readerBuffer := make([]byte, 8192)
	writerBuffer := make([]byte, 0, 8192)

	// Seek to start of sound data
	_, err = wav.file.Seek(58, io.SeekStart)
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
		pos, err = wav.file.Seek(0, io.SeekCurrent)
		if err != nil {
			return err
		}

		// Read 8192 bytes if possible
		n, err = wav.file.Read(readerBuffer)
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

			err = binary.Read(byteReader, binary.LittleEndian, &f)
			if err != nil {
				if err != io.EOF {
					return err
				}
				break
			}

			err = binary.Write(writer, binary.LittleEndian, f*oom)
			if err != nil {
				return err
			}
		}

		// Seek last pos
		_, err = wav.file.Seek(pos, io.SeekStart)
		if err != nil {
			return err
		}

		// Overwrite with normalized samples
		_, err = wav.file.Write(writer.Bytes())
		if err != nil {
			return err
		}
	}

	return nil
}

func (wav *Wav) Write(items []float32) error {
	var buf bytes.Buffer

	for _, item := range items {
		_ = binary.Write(&buf, binary.LittleEndian, item)
	}

	wav.totalSamples += uint32(len(items))

	_, err := wav.file.Write(buf.Bytes())

	return err
}
