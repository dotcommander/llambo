package evals

import (
	"context"
)

func fetchRefreshedOfficialCards(ctx context.Context, opts Options, ifeval sourceSnapshot, fetchers officialCardFetchers) (Result, error) {
	statuses, err := refreshOfficialModelCardsWithFetchers(ctx, opts, fetchers)
	if err != nil {
		return Result{}, err
	}
	var result Result
	result, err = fetchCachedOnly(opts, ifeval)
	if err != nil {
		return Result{}, err
	}
	if err = mergeRefreshedStatuses(result.Sources, statuses); err != nil {
		return Result{}, err
	}
	return result, nil
}

type officialCardFetchers [officialCardCount]officialCardFetcher

func (fetchers officialCardFetchers) resolve(opts Options, index int) officialCardFetcher {
	if fetchers[index] != nil {
		return fetchers[index]
	}
	return officialCardDescriptors[index].resolveFetcher(opts)
}

func loadOfficialCards(ctx context.Context, opts Options, fetchers officialCardFetchers) ([officialCardCount]sourceSnapshot, [officialCardCount]SourceStatus) {
	var cards [officialCardCount]sourceSnapshot
	var cardStatuses [officialCardCount]SourceStatus
	for i, descriptor := range officialCardDescriptors {
		if opts.Refresh {
			fetcher := fetchers.resolve(opts, i)
			cards[i], cardStatuses[i] = loadOptionalOfficialSource(ctx, opts, descriptor.cacheName, func(ctx context.Context) (sourceSnapshot, error) {
				return fetcher(ctx, opts)
			})
		} else {
			cards[i], cardStatuses[i] = readOptionalOfficialSnapshot(opts, descriptor.cacheName)
		}
	}
	return cards, cardStatuses
}
