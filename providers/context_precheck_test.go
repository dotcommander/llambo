package providers

import "testing"

func TestContextFits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		contextLength int
		estimated     int
		headroom      int
		want          bool
	}{
		{name: "unknown limit always fits", contextLength: 0, estimated: 1_000_000, headroom: 512, want: true},
		{name: "negative limit always fits", contextLength: -1, estimated: 1_000_000, headroom: 512, want: true},
		{name: "prompt plus headroom under limit fits", contextLength: 8192, estimated: 1000, headroom: 512, want: true},
		{name: "prompt plus headroom equal to limit fits", contextLength: 8192, estimated: 8192 - 512, headroom: 512, want: true},
		{name: "prompt plus headroom over limit dropped", contextLength: 8192, estimated: 8000, headroom: 512, want: false},
		{name: "prompt alone exceeds limit dropped", contextLength: 4096, estimated: 5000, headroom: 512, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := contextFits(tt.contextLength, tt.estimated, tt.headroom); got != tt.want {
				t.Fatalf("contextFits(%d, %d, %d) = %v, want %v", tt.contextLength, tt.estimated, tt.headroom, got, tt.want)
			}
		})
	}
}
