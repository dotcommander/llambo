package providers

import "strings"

// ModelBackendName identifies one model for a validated config provider name.
// Provider names are trim-nonempty and contain no colons; models remain literal.
func ModelBackendName(provider, model string) string {
	return provider + ":" + model
}

// BaseProviderName removes the exact model suffix from a ModelBackendName result.
// Its provider argument must satisfy ModelBackendName's provider-name precondition.
func BaseProviderName(backendName, model string) string {
	suffix := ":" + model
	if strings.HasSuffix(backendName, suffix) {
		return strings.TrimSuffix(backendName, suffix)
	}
	return backendName
}
