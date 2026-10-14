package evals

func mergeWritingBenchModels(models, writingBench []Model) []Model {
	byIdentity := make(map[string][]int, len(models))
	for i, model := range models {
		if identity, ok := externalIdentity(model); ok {
			byIdentity[identity] = append(byIdentity[identity], i)
		}
	}
	benchByIdentity := make(map[string][]Model, len(writingBench))
	for _, model := range writingBench {
		if identity, ok := externalIdentity(model); ok {
			benchByIdentity[identity] = append(benchByIdentity[identity], model)
		}
	}
	for identity, benchmarks := range benchByIdentity {
		indices := byIdentity[identity]
		if len(indices) != 1 || len(benchmarks) != 1 {
			continue
		}
		if models[indices[0]].Benchmarks == nil {
			models[indices[0]].Benchmarks = make(map[string]BenchmarkResult)
		}
		for name, result := range benchmarks[0].Benchmarks {
			result.Identity = benchmarkIdentityMatch(models[indices[0]], benchmarks[0])
			storeBenchmarkResult(models[indices[0]].Benchmarks, name, result)
		}
	}
	return models
}

func mergeWritingEvidenceModels(models, writingBench []Model, corroborators ...[]Model) []Model {
	models = mergeWritingBenchModels(models, writingBench)
	for _, source := range corroborators {
		models = mergeEQBenchCreativeModels(models, source)
	}
	return models
}

// mergeEQBenchCreativeModels permits name-only matching only when the source
// omits organization and both source and destination have exactly one normalized
// name. This is the narrowest safe reconciliation for the official CSV.

func mergeEQBenchCreativeModels(models, eqBench []Model) []Model {
	byIdentity := make(map[string][]int, len(models))
	byName := make(map[string][]int, len(models))
	byKey := make(map[string]int, len(models))
	for i, model := range models {
		byKey[model.Key] = i
		if identity, ok := externalIdentity(model); ok {
			byIdentity[identity] = append(byIdentity[identity], i)
		}
		if name := normalizeIdentityText(model.Name, true); name != "" {
			byName[name] = append(byName[name], i)
		}
	}
	benchIdentity, benchName := indexEQBenchModels(models, eqBench, byKey)

	for identity, rows := range benchIdentity {
		if indices := byIdentity[identity]; len(rows) == 1 && len(indices) == 1 {
			attachBenchmark(&models[indices[0]], rows[0], benchmarkIdentityMatch(models[indices[0]], rows[0]))
		}
	}
	for name, rows := range benchName {
		if rows[0].Organization != "" || len(rows) != 1 || len(byName[name]) != 1 {
			continue
		}
		if existing := models[byName[name][0]].Benchmarks; existing != nil {
			if _, ok := existing[eqbenchCreativeV3]; ok {
				continue
			}
		}
		attachBenchmark(&models[byName[name][0]], rows[0], IdentityMatchNormalized)
	}
	return models
}

func indexEQBenchModels(models, eqBench []Model, byKey map[string]int) (benchIdentity, benchName map[string][]Model) {
	benchIdentity = make(map[string][]Model, len(eqBench))
	benchName = make(map[string][]Model, len(eqBench))
	for _, model := range eqBench {
		if alias, ok := officialCardKeyAlias(model.Name); ok {
			if index, found := byKey[alias]; found {
				attachBenchmark(&models[index], model, IdentityMatchExact)
				continue
			}
		}
		if identity, ok := externalIdentity(model); ok {
			benchIdentity[identity] = append(benchIdentity[identity], model)
		}
		if name := normalizeIdentityText(model.Name, true); name != "" {
			benchName[name] = append(benchName[name], model)
		}
	}
	return benchIdentity, benchName
}

func attachBenchmark(target *Model, source Model, identity IdentityMatch) {
	if target.Benchmarks == nil {
		target.Benchmarks = make(map[string]BenchmarkResult)
	}
	for name, result := range source.Benchmarks {
		result.Identity = identity
		storeBenchmarkResult(target.Benchmarks, name, result)
	}
}
