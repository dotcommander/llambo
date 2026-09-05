package providers

import "testing"

func TestModelBackendName(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		provider string
		model    string
		want     string
	}{
		{name: "ordinary", provider: "openai", model: "gpt-5", want: "openai:gpt-5"},
		{name: "colon-bearing model", provider: "edge-primary", model: "vendor:model", want: "edge-primary:vendor:model"},
		{name: "empty model", provider: "openai", model: "", want: "openai:"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := ModelBackendName(test.provider, test.model); got != test.want {
				t.Fatalf("ModelBackendName(%q, %q) = %q, want %q", test.provider, test.model, got, test.want)
			}
		})
	}
}

func TestBaseProviderName(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		input string
		model string
		want  string
	}{
		{name: "ordinary", input: "openai:gpt-5", model: "gpt-5", want: "openai"},
		{name: "unexpanded", input: "openai", model: "gpt-5", want: "openai"},
		{name: "empty model", input: "openai:", model: "", want: "openai"},
		{name: "colon-bearing model", input: "edge-primary:vendor:model", model: "vendor:model", want: "edge-primary"},
		{name: "suffix nonmatch", input: "edge-primary:vendor:model", model: "other:model", want: "edge-primary:vendor:model"},
		{name: "empty", input: "", model: "", want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := BaseProviderName(test.input, test.model); got != test.want {
				t.Fatalf("BaseProviderName(%q, %q) = %q, want %q", test.input, test.model, got, test.want)
			}
		})
	}
}

func TestBackendIdentityRoundTrip(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		provider string
		model    string
	}{
		{name: "ordinary", provider: "openai", model: "gpt-5"},
		{name: "colon-bearing model", provider: "edge-primary", model: "vendor:model"},
		{name: "empty model", provider: "openai", model: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			backend := ModelBackendName(test.provider, test.model)
			if got := BaseProviderName(backend, test.model); got != test.provider {
				t.Fatalf("BaseProviderName(ModelBackendName(%q, %q), %q) = %q, want %q", test.provider, test.model, test.model, got, test.provider)
			}
		})
	}
}
