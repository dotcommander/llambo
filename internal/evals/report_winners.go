package evals

func selectCategoryWinners(ranking *CategoryRanking, rows []ReportModel) {
	if len(ranking.Entries) == 0 {
		return
	}
	tied := categoryLeaderTieCount(ranking.Entries)
	winner, official := categoryOfficialWinner(rows, ranking.Category, ranking.Entries[:tied])
	if official && !ranking.Entries[winner].Estimated {
		ranking.Status, ranking.Winner = "official", ranking.Entries[winner].Key
	} else {
		ranking.Status, ranking.Winner = "provisional", ranking.Entries[0].Key
	}
	for i := 0; i < tied; i++ {
		candidate := &ranking.Entries[i]
		switch {
		case i == winner:
			candidate.WinnerStatus = ranking.Status
		case official && familyCount(rows, ranking.Category, candidate.Key) >= 2:
			candidate.WinnerStatus = "official_co_winner"
		default:
			candidate.WinnerStatus = "provisional_co_winner"
		}
	}
}

func categoryLeaderTieCount(entries []CategoryRankedModel) int {
	leader := entries[0]
	tied := 1
	for tied < len(entries) {
		candidate := entries[tied]
		if candidate.Score != leader.Score || candidate.TrustedCoverage != leader.TrustedCoverage || candidate.IdentityConfidence != leader.IdentityConfidence {
			break
		}
		tied++
	}
	return tied
}

func categoryOfficialWinner(rows []ReportModel, category string, entries []CategoryRankedModel) (int, bool) {
	for i, entry := range entries {
		if familyCount(rows, category, entry.Key) >= 2 {
			return i, true
		}
	}
	return 0, false
}
