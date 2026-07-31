package providers

import "strings"

// Blocklist holds normalized "provider:model" keys excluded from ping and
// catalog selection. Zero value (nil set) blocks nothing.
type Blocklist struct {
	set map[string]struct{}
}

// NewBlocklist builds a case-insensitive blocklist from "provider:model"
// entries. Entries split on the first ':' so model IDs containing colons
// (e.g. "hf:zai-org/glm-4.7") are preserved. Malformed entries are skipped.
func NewBlocklist(entries []string) Blocklist {
	set := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		provider, model, ok := strings.Cut(e, ":")
		if !ok {
			continue
		}
		provider = strings.TrimSpace(provider)
		model = strings.TrimSpace(model)
		if provider == "" || model == "" {
			continue
		}
		set[blocklistKey(provider, model)] = struct{}{}
	}
	return Blocklist{set: set}
}

// Blocked reports whether provider:model is on the blocklist (case-insensitive).
func (b Blocklist) Blocked(provider, model string) bool {
	if len(b.set) == 0 {
		return false
	}
	_, ok := b.set[blocklistKey(provider, model)]
	return ok
}

func blocklistKey(provider, model string) string {
	return strings.ToLower(provider) + ":" + strings.ToLower(model)
}
