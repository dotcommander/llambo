package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
)

type localVerification struct{ baseURL, model, reference string }

func verifyLocalCommandModels(io *commandIO, o *evalsLocalRunCommand, omlxURL string) (localVerification, error) {
	var err error
	verified := ""
	verifiedReference := ""
	if strings.HasPrefix(o.Model, "omlx/") {
		if strings.TrimSpace(omlxURL) == "" {
			omlxURL = "http://127.0.0.1:8000"
		}
		verified, err = verifyLocalPrimaryModel(io.Context(), omlxURL, o.Model)
		if err != nil {
			return localVerification{}, err
		}
	}
	if o.Suite == evals.LocalSuiteTTS && strings.HasPrefix(o.ReferenceModel, "omlx/") {
		verifiedReference, err = evals.VerifyOMLXExactModel(io.Context(), omlxURL, "", strings.TrimPrefix(o.ReferenceModel, "omlx/"), nil)
		if err != nil {
			return localVerification{}, fmt.Errorf("verify live TTS round-trip ASR model: %w", err)
		}
	}
	return localVerification{baseURL: omlxURL, model: verified, reference: verifiedReference}, nil
}

func verifyLocalPrimaryModel(ctx context.Context, omlxURL, selector string) (string, error) {
	modelID := strings.TrimPrefix(selector, "omlx/")
	verified, err := evals.VerifyOMLXExactModel(ctx, omlxURL, "", modelID, nil)
	if err != nil {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timer.C:
		}
		verified, err = evals.VerifyOMLXExactModel(ctx, omlxURL, "", modelID, nil)
		if err != nil {
			return "", fmt.Errorf("verify live OMLX model after retry: %w", err)
		}
	}
	return verified, nil
}
