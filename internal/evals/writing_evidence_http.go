package evals

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// fetchWritingEvidenceHTTP reads one bounded, successful writing-evidence response.
// Source adapters retain responsibility for interpreting its body and provenance.
func fetchWritingEvidenceHTTP(ctx context.Context, client *http.Client, url, sourceLabel string, maxBytes int64) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", "llambo-evals/1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, nil, fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", sourceLabel, err)
	}
	if int64(len(body)) > maxBytes {
		return nil, nil, fmt.Errorf("%s exceeds %d bytes", sourceLabel, maxBytes)
	}
	return body, resp.Header.Clone(), nil
}
