package evals

import (
	"strings"
	"unicode"
)

var modelAliases = map[string]string{
	"qwen3-6-27b":            "qwen3.6-27b",
	"gemma-4-26b-a4b":        "gemma-4-26b-a4b-it",
	"gpt-oss-20b":            "gpt-oss-20b",
	"gemini-2-5-flash":       "gemini-2.5-flash",
	"gemini-2-5-flash-lite":  "gemini-2.5-flash-lite",
	"gemini-2-5-pro":         "gemini-2.5-pro",
	"gemini-3-flash-preview": "gemini-3-flash-preview",
	"gemini-3-1-flash-lite":  "gemini-3.1-flash-lite",
	"gemini-3-1-pro-preview": "gemini-3.1-pro-preview",
	"gemini-3-5-flash":       "gemini-3.5-flash",
	"gemma-4-26b-a4b-it":     "gemma-4-26b-a4b-it",
}

// organizationFamilies is the reviewed boundary for source-native organization
// labels. Values identify one model-producing family; unrelated organizations
// must never be collapsed merely because a model name happens to match.
var organizationFamilies = map[string]string{
	"alibaba":                 "alibaba",
	"alibaba cloud qwen team": "alibaba",
	"bytedance":               "bytedance",
	"bytedance seed":          "bytedance",
	"kimi":                    "moonshot",
	"moonshot ai":             "moonshot",
	"mistral":                 "mistral",
	"mistral ai":              "mistral",
	"sarvam":                  "sarvam",
	"sarvam ai":               "sarvam",
	"z ai":                    "zhipu",
	"zhipu ai":                "zhipu",
}

func mergeModels(llm, aa []Model) []Model {
	out := append([]Model(nil), llm...)
	llmByIdentity := make(map[string][]int, len(out))
	for i := range out {
		out[i].IdentityMatch = IdentityMatchUnmatched
		if identity, ok := externalIdentity(out[i]); ok {
			llmByIdentity[identity] = append(llmByIdentity[identity], i)
		}
	}
	aaByIdentity := make(map[string][]int, len(aa))
	for i := range aa {
		if identity, ok := externalIdentity(aa[i]); ok {
			aaByIdentity[identity] = append(aaByIdentity[identity], i)
		}
	}

	for identity, indices := range llmByIdentity {
		if len(indices) > 1 || len(aaByIdentity[identity]) > 1 {
			for _, index := range indices {
				out[index].IdentityMatch = IdentityMatchAmbiguous
			}
		}
	}
	for identity, indices := range aaByIdentity {
		if len(indices) > 1 || len(llmByIdentity[identity]) > 1 {
			for _, index := range indices {
				aa[index].IdentityMatch = IdentityMatchAmbiguous
			}
		}
	}

	for i, model := range aa {
		if model.AA != nil {
			model.AA.sourceName = model.Name
			model.AA.sourceOrganization = model.Organization
		}
		identity, hasIdentity := externalIdentity(model)
		llmIndices := llmByIdentity[identity]
		aaIndices := aaByIdentity[identity]
		if hasIdentity && len(llmIndices) == 1 && len(aaIndices) == 1 {
			index := llmIndices[0]
			out[index].AA = model.AA
			out[index].IdentityMatch = IdentityMatchNormalized
			if normalizeIdentityText(out[index].Key, true) == normalizeIdentityText(model.Key, true) {
				out[index].IdentityMatch = IdentityMatchExact
			}
			continue
		}
		if model.IdentityMatch == "" {
			model.IdentityMatch = IdentityMatchUnmatched
		}
		model.Key = "aa:" + canonicalKey(model.Key)
		aa[i] = model
		out = append(out, model)
	}
	return out
}

func externalIdentity(model Model) (string, bool) {
	name := normalizeIdentityText(model.Name, true)
	organization := canonicalOrganizationFamily(model.Organization)
	if name == "" || organization == "" {
		return "", false
	}
	return organization + "\x00" + name, true
}

func canonicalOrganizationFamily(organization string) string {
	normalized := normalizeIdentityText(organization, false)
	if normalized == "" {
		return ""
	}
	if family, ok := organizationFamilies[normalized]; ok {
		return family
	}
	return normalized
}

// normalizeIdentityText normalizes formatting differences without erasing
// semantic qualifiers. In particular, '+' remains part of a model name so
// variants such as Command A and Command A+ cannot collide.
func normalizeIdentityText(value string, preservePlus bool) string {
	var b strings.Builder
	separator := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || (preservePlus && r == '+') {
			if separator && b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteRune(r)
			separator = false
			continue
		}
		separator = true
	}
	return b.String()
}

func canonicalKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", "-")
	if alias, ok := modelAliases[s]; ok {
		return alias
	}
	return s
}
