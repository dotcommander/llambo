package gateway

// JobStats is one consistent snapshot of job-state counters.
type JobStats struct {
	Total      int
	Pending    int
	Processing int
	Completed  int
	Failed     int
	Cancelled  int
}

// Stats returns job statistics
func (m *JobManager) Stats() JobStats {
	var stats JobStats
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats.Total = len(m.jobs)
	for _, job := range m.jobs {
		job.mu.RLock()
		switch job.Status {
		case JobStatusPending:
			stats.Pending++
		case JobStatusProcessing:
			stats.Processing++
		case JobStatusCompleted:
			stats.Completed++
		case JobStatusFailed:
			stats.Failed++
		case JobStatusCancelled:
			stats.Cancelled++
		case JobStatusPartialFailure:
			// Terminal with mixed per-request outcomes; counted as stats.Completed.
			stats.Completed++
		}
		job.mu.RUnlock()
	}
	return stats
}
