package cmd

import (
	"slices"
	"testing"
	"time"
)

func TestRunOrderedParallel_PreservesInputOrder(t *testing.T) {
	items := []int{1, 2, 3, 4}

	results := runOrderedParallel(items, func(_ int, item int) string {
		switch item {
		case 1:
			time.Sleep(20 * time.Millisecond)
		case 2:
			time.Sleep(15 * time.Millisecond)
		case 3:
			time.Sleep(10 * time.Millisecond)
		}
		return string(rune('0' + item))
	})

	if !slices.Equal(results, []string{"1", "2", "3", "4"}) {
		t.Fatalf("unexpected result order: %v", results)
	}
}

func TestRunOrderedParallel_Empty(t *testing.T) {
	results := runOrderedParallel([]int(nil), func(_ int, item int) int {
		return item * 2
	})

	if len(results) != 0 {
		t.Fatalf("expected empty results, got %v", results)
	}
}
