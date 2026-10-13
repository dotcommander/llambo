package providers

import (
	"context"
	"testing"
)

func TestIsGemini3SeriesModel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		model string
		want  bool
	}{
		{"gemini-2.5-flash-lite", false},
		{"gemini-2.5-pro", false},
		{"gemini-3.6-flash", true},
		{"gemini-3.7-flash", true},
		{"gemini-3-pro-preview", true},
		{"models/gemini-3.6-flash", true},
		{"Gemini-3.6-Flash", true},
		{"gemini-fixture", false},
		{"gpt-4o", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isGemini3SeriesModel(tc.model); got != tc.want {
			t.Errorf("isGemini3SeriesModel(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}

func TestBuildTextRequestDropsTemperatureOnGemini3Series(t *testing.T) {
	t.Parallel()
	temperature := 0.7
	ctx := WithChatRequestOverrides(context.Background(), nil, &temperature, nil)

	three := buildTextRequest(ctx, Config{Model: "gemini-3.6-flash", ProviderType: "gemini", Temperature: 0.7}, "system", "user")
	if three.Temperature != nil {
		t.Fatalf("gemini-3.6-flash temperature = %v, want unset", *three.Temperature)
	}

	twoFive := buildTextRequest(ctx, Config{Model: "gemini-2.5-flash", ProviderType: "gemini", Temperature: 0.7}, "system", "user")
	if twoFive.Temperature == nil || *twoFive.Temperature != 0.7 {
		t.Fatalf("gemini-2.5-flash temperature = %+v, want 0.7", twoFive.Temperature)
	}

	openai := buildTextRequest(ctx, Config{Model: "gpt-4o", ProviderType: "openai", Temperature: 0.7}, "system", "user")
	if openai.Temperature == nil || *openai.Temperature != 0.7 {
		t.Fatalf("gpt-4o temperature = %+v, want 0.7", openai.Temperature)
	}
}
