package evals

import (
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOMLXAudioAdapterASRAndTTS(t *testing.T) {
	t.Parallel()
	wav := validTestWAV()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/audio/transcriptions":
			if err := r.ParseMultipartForm(int64(len(wav) + 4096)); err != nil {
				t.Error(err)
			}
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
			} else {
				defer file.Close()
				got, _ := io.ReadAll(file)
				if string(got) != string(wav) {
					t.Error("ASR did not send sealed WAV")
				}
			}
			w.Header().Set("X-Llambo-Provider", "omlx")
			w.Header().Set("X-Llambo-Model", "asr")
			_, _ = w.Write([]byte(`{"text":"Hello, world!"}`))
		case "/v1/audio/speech":
			w.Header().Set("X-Llambo-Provider", "omlx")
			w.Header().Set("X-Llambo-Model", "tts")
			_, _ = w.Write(wav)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	adapter := OMLXAudioAdapter{BaseURL: server.URL, Client: server.Client()}
	asr, err := adapter.Transcribe(context.Background(), "omlx/asr", ASRAudioInput{Case: ASRCase{ID: "clip", Transcript: "hello world"}, WAV: wav})
	if err != nil || asr.WER != 0 || asr.RealTimeFactor <= 0 {
		t.Fatalf("ASR = %#v, %v", asr, err)
	}
	tts, err := adapter.Synthesize(context.Background(), TTSSpeechRequest{Model: "omlx/tts", Input: "hello world"}, audioRoundTripFunc(func(context.Context, []byte) (string, error) { return "hello world", nil }))
	if err != nil || tts.IntelligibilityWER == nil || *tts.IntelligibilityWER != 0 || tts.WAVBase64 == "" {
		t.Fatalf("TTS = %#v, %v", tts, err)
	}
	roundTrip := OMLXTTSRoundTripper{Adapter: adapter, Model: "omlx/asr"}
	transcript, err := roundTrip.TranscribeGeneratedAudio(context.Background(), wav)
	if err != nil || transcript != "Hello, world!" {
		t.Fatalf("round trip = %q, %v", transcript, err)
	}
}

func TestLoadSealedWAVRejectsAmbiguousInput(t *testing.T) {
	t.Parallel()
	if _, _, err := loadSealedWAV(ASRAudioInput{WAV: validTestWAV(), WAVPath: "sealed.wav"}); err == nil {
		t.Fatal("data and path accepted together")
	}
}

type audioRoundTripFunc func(context.Context, []byte) (string, error)

func (f audioRoundTripFunc) TranscribeGeneratedAudio(ctx context.Context, wav []byte) (string, error) {
	return f(ctx, wav)
}

func validTestWAV() []byte {
	wav := make([]byte, 48)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:8], uint32(len(wav)-8))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:20], 16)
	binary.LittleEndian.PutUint16(wav[20:22], 1)
	binary.LittleEndian.PutUint16(wav[22:24], 1)
	binary.LittleEndian.PutUint32(wav[24:28], 8000)
	binary.LittleEndian.PutUint32(wav[28:32], 16000)
	binary.LittleEndian.PutUint16(wav[32:34], 2)
	binary.LittleEndian.PutUint16(wav[34:36], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:44], 4)
	return wav
}
