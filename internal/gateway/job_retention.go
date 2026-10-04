package gateway

import (
	"net/http"
	"sort"
	"time"
)

func (m *JobManager) SetRetentionLimits(count int, bytes int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if count > 0 {
		m.maxRetained = count
	}
	if bytes > 0 {
		m.maxRetainedBytes = bytes
	}
	m.enforceRetentionLocked(time.Now().Add(-JobMaxAge))
}

// Caller holds the manager lock. Active and draining jobs always remain resident.
func (m *JobManager) enforceRetentionLocked(cutoff time.Time) int {
	type candidate struct {
		id      string
		updated time.Time
		bytes   int64
	}
	var finished []candidate
	var totalBytes int64
	removed := 0
	for id, job := range m.jobs {
		job.mu.RLock()
		drained := job.drained
		terminal := job.Status.IsTerminal()
		updated := job.UpdatedAt
		bytes := job.retainedBytesLocked()
		job.mu.RUnlock()
		if terminal && drained && updated.Before(cutoff) {
			delete(m.jobs, id)
			removed++
			continue
		}
		totalBytes += bytes
		if terminal && drained {
			finished = append(finished, candidate{id, updated, bytes})
		}
	}
	sort.Slice(finished, func(i, j int) bool {
		if finished[i].updated.Equal(finished[j].updated) {
			return finished[i].id < finished[j].id
		}
		return finished[i].updated.Before(finished[j].updated)
	})
	for _, job := range finished {
		if len(m.jobs) <= m.maxRetained && totalBytes <= m.maxRetainedBytes {
			break
		}
		delete(m.jobs, job.id)
		totalBytes -= job.bytes
		removed++
	}
	return removed
}
func (m *JobManager) CancelJobStatus(id string) (*Job, int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	job := m.jobs[id]
	if job == nil {
		return nil, http.StatusNotFound
	}
	if _, ok := m.cancel(job); !ok {
		return job, http.StatusConflict
	}
	return job, http.StatusOK
}
