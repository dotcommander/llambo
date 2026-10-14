package evals

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
)

type sealedTranscriptionInput struct {
	wav      []byte
	filename string
	metadata WAVMetadata
}

func sealedTranscriptionAudio(input ASRAudioInput) (sealedTranscriptionInput, error) {
	wav, filename, err := loadSealedWAV(input)
	if err != nil {
		return sealedTranscriptionInput{}, err
	}
	if input.WAVPath != "" {
		got := fmt.Sprintf("%x", sha256.Sum256(wav))
		if input.Case.WAVSHA256 == "" || !strings.EqualFold(got, input.Case.WAVSHA256) {
			return sealedTranscriptionInput{}, errors.New("sealed WAV SHA-256 mismatch")
		}
	}
	metadata, err := ValidateWAV(wav)
	if err != nil {
		return sealedTranscriptionInput{}, fmt.Errorf("validate sealed WAV: %w", err)
	}
	return sealedTranscriptionInput{wav: wav, filename: filename, metadata: metadata}, nil
}

func transcriptionForm(model string, input sealedTranscriptionInput) (*bytes.Buffer, string, error) {
	body := new(bytes.Buffer)
	form := multipart.NewWriter(body)
	if err := form.WriteField("model", model); err != nil {
		return nil, "", err
	}
	part, err := form.CreateFormFile("file", input.filename)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(input.wav); err != nil {
		return nil, "", err
	}
	if err := form.Close(); err != nil {
		return nil, "", err
	}
	return body, form.FormDataContentType(), nil
}

func loadSealedWAV(input ASRAudioInput) ([]byte, string, error) {
	if len(input.WAV) > 0 && strings.TrimSpace(input.WAVPath) != "" {
		return nil, "", errors.New("provide sealed WAV data or path, not both")
	}
	wav := input.WAV
	filename := input.FileName
	if input.WAVPath != "" {
		data, err := os.ReadFile(input.WAVPath)
		if err != nil {
			return nil, "", fmt.Errorf("read sealed WAV: %w", err)
		}
		wav = data
		if filename == "" {
			filename = filepath.Base(input.WAVPath)
		}
	}
	if len(wav) == 0 || len(wav) > localAudioLimit {
		return nil, "", fmt.Errorf("sealed WAV must be between 1 and %d bytes", localAudioLimit)
	}
	if filename == "" {
		filename = "sealed.wav"
	}
	return append([]byte(nil), wav...), filename, nil
}
