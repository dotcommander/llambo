package evals

import "sort"

func sealLLMStatsBlockedReceipts(root string, blocked []map[string]string) error {
	sort.Slice(blocked, func(i, j int) bool { return blocked[i][benchmark1] < blocked[j][benchmark1] })
	if len(blocked) == 0 {
		return nil
	}
	receipts, err := encodeJSONLMaps(blocked)
	if err != nil {
		return err
	}
	return writeSealed(sealedRevisionPath(root, blocked1, receipts, ".jsonl"), receipts)
}
