package providers

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestKeyRotator_SingleKey(t *testing.T) {
	t.Parallel()
	// Use provider name that won't match env vars
	configs := map[string]Config{
		"testprovider": {APIKey: "sk-single"},
	}

	kr := NewKeyRotator(configs)

	assert.Equal(t, "sk-single", kr.GetKey("testprovider"))
	assert.Equal(t, 1, kr.KeyCount("testprovider"))

	// Can't rotate with single key
	assert.False(t, kr.MarkRateLimited("testprovider", kr.GetKey("testprovider")))
}

func TestKeyRotator_MultipleKeys(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{
		"testprovider": {APIKeys: []string{"sk-key1", "sk-key2", "sk-key3"}},
	}

	kr := NewKeyRotator(configs)

	assert.Equal(t, "sk-key1", kr.GetKey("testprovider"))
	assert.Equal(t, 3, kr.KeyCount("testprovider"))

	// Rotate to next key
	assert.True(t, kr.MarkRateLimited("testprovider", kr.GetKey("testprovider")))
	assert.Equal(t, "sk-key2", kr.GetKey("testprovider"))

	// Rotate again
	assert.True(t, kr.MarkRateLimited("testprovider", kr.GetKey("testprovider")))
	assert.Equal(t, "sk-key3", kr.GetKey("testprovider"))

	// All keys exhausted
	assert.False(t, kr.MarkRateLimited("testprovider", kr.GetKey("testprovider")))
}

func TestKeyRotator_RecordSuccess(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{
		"testprovider": {APIKeys: []string{"sk-key1", "sk-key2"}},
	}

	kr := NewKeyRotator(configs)

	// Rate limit first key, rotate to second
	assert.True(t, kr.MarkRateLimited("testprovider", kr.GetKey("testprovider")))
	assert.Equal(t, "sk-key2", kr.GetKey("testprovider"))

	// Success clears rate limit flag on current key
	kr.RecordSuccess("testprovider")

	states := kr.GetKeyStates("testprovider")
	assert.Len(t, states, 2)
	assert.True(t, states[0].RateLimited)  // first key still limited
	assert.False(t, states[1].RateLimited) // second key cleared
}

func TestKeyRotator_RecordSuccessForExactLateKey(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{
		"testprovider": {APIKeys: []string{"sk-key1", "sk-key2"}},
	}
	kr := NewKeyRotator(configs)

	if !kr.MarkRateLimited("testprovider", "sk-key1") {
		t.Fatal("first key did not rotate")
	}
	if kr.MarkRateLimited("testprovider", "sk-key2") {
		t.Fatal("second key unexpectedly remained available")
	}

	kr.RecordSuccessForKey("testprovider", "sk-key1")
	states := kr.GetKeyStates("testprovider")
	if states[0].RateLimited {
		t.Fatal("late success did not restore its exact leased key")
	}
	if !states[1].RateLimited {
		t.Fatal("late success incorrectly restored the current key")
	}
}

func TestKeyRotator_CooldownRecovery(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{
		"testprovider": {APIKeys: []string{"sk-key1", "sk-key2"}},
	}

	kr := NewKeyRotator(configs)
	kr.cooldown = 10 * time.Millisecond // short cooldown for test

	// Exhaust all keys
	kr.MarkRateLimited("testprovider", kr.GetKey("testprovider"))
	kr.MarkRateLimited("testprovider", kr.GetKey("testprovider"))
	assert.False(t, kr.HasAvailableKeys("testprovider"))

	// Wait for cooldown
	time.Sleep(15 * time.Millisecond)

	// Keys should be available again
	assert.True(t, kr.HasAvailableKeys("testprovider"))
}

func TestKeyRotator_MixedKeySources(t *testing.T) {
	t.Parallel()
	// Test that api_keys and api_key get combined
	configs := map[string]Config{
		"testprovider": {
			APIKeys: []string{"sk-from-array"},
			APIKey:  "sk-from-single",
		},
	}

	kr := NewKeyRotator(configs)

	// Should have both keys
	assert.Equal(t, 2, kr.KeyCount("testprovider"))
	assert.Equal(t, "sk-from-array", kr.GetKey("testprovider"))

	kr.MarkRateLimited("testprovider", kr.GetKey("testprovider"))
	assert.Equal(t, "sk-from-single", kr.GetKey("testprovider"))
}

func TestKeyRotator_GetKeyStates_MasksKeys(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{
		"testprovider": {APIKeys: []string{"sk-verylongkey12345678"}},
	}

	kr := NewKeyRotator(configs)
	states := kr.GetKeyStates("testprovider")

	assert.Len(t, states, 1)
	assert.Equal(t, "sk-v...5678", states[0].Key) // masked for security
}

// TestKeyRotator_ConcurrentRateLimit verifies that when many goroutines hit a
// 429 on the same key concurrently, the rotator advances exactly once: callers
// that used a key already rotated past must not double-advance the index.
func TestKeyRotator_ConcurrentRateLimit(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{
		"testprovider": {APIKeys: []string{"sk-key1", "sk-key2", "sk-key3"}},
	}

	kr := NewKeyRotator(configs)

	// All goroutines obtained "sk-key1" before any 429 occurred.
	usedKey := kr.GetKey("testprovider")
	assert.Equal(t, "sk-key1", usedKey)

	const goroutines = 16
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			kr.MarkRateLimited("testprovider", usedKey)
		}()
	}
	wg.Wait()

	// Exactly one rotation should have happened: only sk-key1 is rate-limited,
	// the rotator now points at sk-key2, and sk-key3 is untouched.
	assert.Equal(t, "sk-key2", kr.GetKey("testprovider"))

	states := kr.GetKeyStates("testprovider")
	assert.Len(t, states, 3)
	assert.True(t, states[0].RateLimited, "sk-key1 should be rate-limited")
	assert.False(t, states[1].RateLimited, "sk-key2 should remain healthy")
	assert.False(t, states[2].RateLimited, "sk-key3 should not have been skipped")
}

func TestKeyRotator_UnknownProvider(t *testing.T) {
	t.Parallel()
	configs := map[string]Config{}

	kr := NewKeyRotator(configs)

	assert.Equal(t, "", kr.GetKey("unknown"))
	assert.Equal(t, 0, kr.KeyCount("unknown"))
	assert.False(t, kr.MarkRateLimited("unknown", ""))
	assert.False(t, kr.HasAvailableKeys("unknown"))
}
