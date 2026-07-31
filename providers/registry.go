package providers

import (
	"sort"
)

// GetProviderOrderFromConfigs returns provider names sorted by priority from config
func GetProviderOrderFromConfigs(configs map[string]Config) []string {
	type priorityEntry struct {
		name     string
		priority int
	}

	var entries []priorityEntry
	for name, cfg := range configs {
		entries = append(entries, priorityEntry{name: name, priority: cfg.Priority})
	}

	// Sort by priority (lower = higher priority), then by name for stability
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].priority != entries[j].priority {
			return entries[i].priority < entries[j].priority
		}
		return entries[i].name < entries[j].name
	})

	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.name
	}
	return names
}
