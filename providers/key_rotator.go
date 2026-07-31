package providers

import (
	"sync"
	"time"
)

// KeyState tracks the health state of a single API key
type KeyState struct {
	Key         string
	RateLimited bool
	LimitedAt   time.Time
	Failures    int
}

// KeyRotator manages multiple API keys per provider with 429 rotation
type KeyRotator struct {
	keys     map[string][]*KeyState // provider name -> key states
	current  map[string]int         // provider name -> current key index
	mu       sync.RWMutex
	cooldown time.Duration
}

// NewKeyRotator creates a key rotator for the given provider configs
func NewKeyRotator(configs map[string]Config) *KeyRotator {
	kr := &KeyRotator{
		keys:     make(map[string][]*KeyState),
		current:  make(map[string]int),
		cooldown: RateLimitCooldown,
	}

	for name, cfg := range configs {
		apiKeys := GetAPIKeys(name, cfg)
		if len(apiKeys) == 0 {
			continue
		}

		states := make([]*KeyState, len(apiKeys))
		for i, k := range apiKeys {
			states[i] = &KeyState{Key: k}
		}
		kr.keys[name] = states
		kr.current[name] = 0
	}

	return kr
}

// GetKey returns the current active API key for a provider
func (kr *KeyRotator) GetKey(provider string) string {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	keys := kr.keys[provider]
	if len(keys) == 0 {
		return ""
	}

	idx := kr.current[provider]
	if idx >= len(keys) {
		idx = 0
	}
	return keys[idx].Key
}

// KeyCount returns the number of keys for a provider
func (kr *KeyRotator) KeyCount(provider string) int {
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	return len(kr.keys[provider])
}

// NextKeyForRotation reports the key that would replace usedKey without
// changing rotator state. The caller must construct its replacement client
// first, then use CommitRateLimit to make the selection visible.
func (kr *KeyRotator) NextKeyForRotation(provider, usedKey string) (string, bool) {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	keys := kr.keys[provider]
	if len(keys) == 0 {
		return "", false
	}
	idx := kr.current[provider]
	if idx >= len(keys) {
		idx = 0
	}
	if keys[idx].Key != usedKey {
		return keys[idx].Key, !keys[idx].RateLimited || time.Since(keys[idx].LimitedAt) > kr.cooldown
	}
	for offset := 1; offset < len(keys); offset++ {
		state := keys[(idx+offset)%len(keys)]
		if !state.RateLimited || time.Since(state.LimitedAt) > kr.cooldown {
			return state.Key, true
		}
	}
	return "", false
}

// CommitRateLimit marks usedKey rate-limited and selects candidateKey only
// when usedKey is still current. It is the state-changing half of a rotation
// after the replacement client has been constructed successfully.
func (kr *KeyRotator) CommitRateLimit(provider, usedKey, candidateKey string) bool {
	kr.mu.Lock()
	defer kr.mu.Unlock()

	keys := kr.keys[provider]
	if len(keys) == 0 {
		return false
	}
	idx := kr.current[provider]
	if idx >= len(keys) {
		idx = 0
	}
	if keys[idx].Key != usedKey {
		return false
	}

	candidateIdx := -1
	for offset := 1; offset < len(keys); offset++ {
		i := (idx + offset) % len(keys)
		if keys[i].Key == candidateKey {
			candidateIdx = i
			break
		}
	}
	if candidateIdx < 0 {
		return false
	}
	candidate := keys[candidateIdx]
	if candidate.RateLimited && time.Since(candidate.LimitedAt) <= kr.cooldown {
		return false
	}

	keys[idx].RateLimited = true
	keys[idx].LimitedAt = time.Now()
	if candidate.RateLimited {
		candidate.RateLimited = false
		candidate.Failures = 0
	}
	kr.current[provider] = candidateIdx
	return true
}

// MarkRateLimited marks usedKey as rate-limited and rotates to the next key.
// usedKey is the key the caller actually obtained from GetKey before the 429.
//
// Concurrency: when several goroutines hit a 429 on the same provider key at
// once, only the first should advance the rotation. If kr.current no longer
// points at usedKey, another goroutine already rotated away from it, so this
// call is a no-op (it does not double-advance) and simply reports whether a
// non-rate-limited key is currently available.
//
// Returns true if a usable key is available, false if all keys are exhausted.
func (kr *KeyRotator) MarkRateLimited(provider, usedKey string) bool {
	kr.mu.Lock()
	defer kr.mu.Unlock()

	keys := kr.keys[provider]
	if len(keys) == 0 {
		return false
	}

	idx := kr.current[provider]
	if idx >= len(keys) {
		idx = 0
	}

	// If the current key is no longer the one the caller used, another
	// goroutine already rotated past it. Do not advance again; just report
	// whether the now-current key is usable.
	if keys[idx].Key != usedKey {
		return !keys[idx].RateLimited
	}

	// Mark the used key as rate limited.
	keys[idx].RateLimited = true
	keys[idx].LimitedAt = time.Now()

	// Try to find next available key.
	return kr.rotateToNextAvailable(provider)
}

// rotateToNextAvailable finds the next non-rate-limited key.
// Must be called with lock held. Returns true if found, false if all exhausted.
func (kr *KeyRotator) rotateToNextAvailable(provider string) bool {
	keys := kr.keys[provider]
	if len(keys) == 0 {
		return false
	}

	startIdx := kr.current[provider]
	checked := 0

	for checked < len(keys) {
		nextIdx := (startIdx + checked + 1) % len(keys)
		state := keys[nextIdx]

		// Check if this key has recovered from rate limit
		if state.RateLimited {
			if time.Since(state.LimitedAt) > kr.cooldown {
				// Key cooldown expired, reset it
				state.RateLimited = false
				state.Failures = 0
			}
		}

		if !state.RateLimited {
			kr.current[provider] = nextIdx
			return true
		}
		checked++
	}

	return false
}

// RecordSuccess marks the current key as healthy
func (kr *KeyRotator) RecordSuccess(provider string) {
	kr.RecordSuccessForKey(provider, kr.GetKey(provider))
}

// RecordSuccessForKey marks exactly the key that served a successful request
// healthy. Late success from a retired generation must not mutate the current
// key's state.
func (kr *KeyRotator) RecordSuccessForKey(provider, key string) {
	kr.mu.Lock()
	defer kr.mu.Unlock()

	keys := kr.keys[provider]
	if len(keys) == 0 || key == "" {
		return
	}
	for _, state := range keys {
		if state.Key == key {
			state.RateLimited = false
			state.Failures = 0
			return
		}
	}
}

// HasAvailableKeys returns true if any keys are available (not rate-limited or cooldown expired)
func (kr *KeyRotator) HasAvailableKeys(provider string) bool {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	keys := kr.keys[provider]
	if len(keys) == 0 {
		return false
	}

	for _, state := range keys {
		if !state.RateLimited {
			return true
		}
		if time.Since(state.LimitedAt) > kr.cooldown {
			return true
		}
	}
	return false
}

// GetKeyStates returns the current state of all keys for a provider (for debugging/health)
func (kr *KeyRotator) GetKeyStates(provider string) []KeyState {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	keys := kr.keys[provider]
	states := make([]KeyState, len(keys))
	for i, s := range keys {
		states[i] = *s
		// Mask the key for security
		if len(states[i].Key) > 8 {
			states[i].Key = states[i].Key[:4] + "..." + states[i].Key[len(states[i].Key)-4:]
		}
	}
	return states
}
