package cmd

import "sync"

func runOrderedParallel[T any, R any](items []T, fn func(int, T) R) []R {
	results := make([]R, len(items))
	var wg sync.WaitGroup

	for i, item := range items {
		wg.Add(1)
		go func(idx int, current T) {
			defer wg.Done()
			results[idx] = fn(idx, current)
		}(i, item)
	}

	wg.Wait()
	return results
}

// runOrderedProviderGroups fans out across providers while preserving target
// order within a provider. This avoids opening an unbounded burst of requests
// to one provider when a selector expands to many of its models.
func runOrderedProviderGroups[T any, R any](items []T, provider func(T) string, fn func(int, T) R) []R {
	results := make([]R, len(items))
	groups := make(map[string][]int)
	order := make([]string, 0, len(items))
	for index, item := range items {
		name := provider(item)
		if _, ok := groups[name]; !ok {
			order = append(order, name)
		}
		groups[name] = append(groups[name], index)
	}

	var wg sync.WaitGroup
	for _, name := range order {
		indexes := groups[name]
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, index := range indexes {
				results[index] = fn(index, items[index])
			}
		}()
	}
	wg.Wait()
	return results
}
