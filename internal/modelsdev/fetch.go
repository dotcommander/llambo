package modelsdev

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultURL is the models.dev aggregated model/pricing catalog.
const DefaultURL = "https://models.dev/api.json"

const maxBodyBytes = 16 << 20 // 16MB cap on the response body

var fetchClient = &http.Client{Timeout: 30 * time.Second}

// Fetch retrieves the raw api.json body from url, propagating ctx and capping size.
func Fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := fetchClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("models.dev fetch %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
}
