package evals

import (
	"encoding/binary"
	"errors"
	"math"
)

// WAVMetadata describes a validated WAV payload.
type WAVMetadata struct {
	SampleRate      uint32
	Channels        uint16
	BitsPerSample   uint16
	DurationSeconds float64
	DataBytes       uint32
}

// ValidateWAV checks RIFF chunks and derives duration without altering the payload.
func ValidateWAV(data []byte) (WAVMetadata, error) {
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return WAVMetadata{}, errors.New("not a RIFF/WAVE file")
	}
	parsed, err := parseWAVChunks(data)
	if err != nil {
		return WAVMetadata{}, err
	}
	f := parsed.metadata
	if !parsed.foundFmt || !parsed.foundData || f.Channels == 0 || f.SampleRate == 0 || f.BitsPerSample == 0 || f.DataBytes == 0 {
		return WAVMetadata{}, errors.New("WAV requires nonempty fmt and data chunks")
	}
	bytesPerSecond := float64(f.SampleRate) * float64(f.Channels) * float64(f.BitsPerSample) / 8
	f.DurationSeconds = float64(f.DataBytes) / bytesPerSecond
	if f.DurationSeconds <= 0 || math.IsInf(f.DurationSeconds, 0) || math.IsNaN(f.DurationSeconds) {
		return WAVMetadata{}, errors.New("invalid WAV duration")
	}
	return f, nil
}

type parsedWAVChunks struct {
	metadata            WAVMetadata
	foundFmt, foundData bool
}

func parseWAVChunks(data []byte) (parsedWAVChunks, error) {
	var parsed parsedWAVChunks
	for p := 12; p+8 <= len(data); {
		id := string(data[p : p+4])
		n := int(binary.LittleEndian.Uint32(data[p+4 : p+8]))
		p += 8
		if n < 0 || n > len(data) || p+n > len(data) {
			return parsedWAVChunks{}, errors.New("invalid WAV chunk")
		}
		chunk := data[p : p+n]
		switch id {
		case "fmt ":
			if len(chunk) < 16 {
				return parsedWAVChunks{}, errors.New("short WAV fmt chunk")
			}
			parsed.metadata.Channels = binary.LittleEndian.Uint16(chunk[2:4])
			parsed.metadata.SampleRate = binary.LittleEndian.Uint32(chunk[4:8])
			parsed.metadata.BitsPerSample = binary.LittleEndian.Uint16(chunk[14:16])
			parsed.foundFmt = true
		case "data":
			parsed.metadata.DataBytes = uint32(n) //nolint:gosec // bounded by construction
			parsed.foundData = true
		}
		p += n
		if n%2 == 1 {
			p++
		}
	}
	return parsed, nil
}
