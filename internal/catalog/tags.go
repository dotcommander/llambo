package catalog

import (
	"sort"
	"strings"
)

func AddTag(m *ModelEntry, tag string) {
	tag = normalizeTag(tag)
	if tag == "" || HasTag(m, tag) {
		return
	}
	m.Tags = append(m.Tags, tag)
	sort.Strings(m.Tags)
}

func RemoveTag(m *ModelEntry, tag string) {
	tag = normalizeTag(tag)
	if tag == "" || m == nil {
		return
	}
	kept := m.Tags[:0]
	for _, existing := range m.Tags {
		if normalizeTag(existing) != tag {
			kept = append(kept, existing)
		}
	}
	m.Tags = kept
}

func HasTag(m *ModelEntry, tag string) bool {
	tag = normalizeTag(tag)
	if tag == "" || m == nil {
		return false
	}
	for _, existing := range m.Tags {
		if normalizeTag(existing) == tag {
			return true
		}
	}
	return false
}

func normalizeTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}
