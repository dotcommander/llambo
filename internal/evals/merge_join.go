package evals

func mergeModels(llm, aa []Model) []Model {
	out := append([]Model(nil), llm...)
	llmByIdentity, aaByIdentity := indexExternalModels(out, aa)

	for i, model := range aa {
		if attachArtificialModel(out, model, llmByIdentity, aaByIdentity) {
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

// mergeWritingBenchModels only attaches source-native scores when both sides
// have one conservative identity match. Ambiguous or unmatched rows stay out
// of the capability report instead of guessing an identity.

func indexExternalModels(out, aa []Model) (llmByIdentity, aaByIdentity map[string][]int) {
	llmByIdentity = make(map[string][]int, len(out))
	for i := range out {
		out[i].IdentityMatch = IdentityMatchUnmatched
		if identity, ok := externalIdentity(out[i]); ok {
			llmByIdentity[identity] = append(llmByIdentity[identity], i)
		}
	}
	aaByIdentity = make(map[string][]int, len(aa))
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

	return llmByIdentity, aaByIdentity
}

func attachArtificialModel(out []Model, model Model, llmByIdentity, aaByIdentity map[string][]int) bool {
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
		return true
	}
	return false
}
