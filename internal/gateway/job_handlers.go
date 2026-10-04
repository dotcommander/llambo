package gateway

import (
	"errors"
	"fmt"
	"github.com/dotcommander/llambo/providers"
	"net/http"
	"strings"
)

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBodySize)
	var req CreateJobRequest
	if err := decodeSingleJSON(r.Body, &req, false); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body: "+err.Error())
		return
	}
	maxRequests := s.maxRequestsPerJob
	if maxRequests <= 0 {
		maxRequests = JobMaxRequestsPerJob
	}
	if len(req.Requests) > maxRequests {
		writeError(w, http.StatusBadRequest, "invalid_request", "Too many requests in job: reduce batch size")
		return
	}
	if err := validateJobBatch(req.Requests); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	target, err := providers.ResolveTarget(s.configs, req.Model, "")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	job, err := s.jobManager.createTargetJob(r.Context(), req.Requests, req.SystemPrompt, true, target)
	if err != nil {
		if errors.Is(err, ErrJobManagerShuttingDown) {
			writeError(w, http.StatusServiceUnavailable, "server_shutting_down", "Job manager is shutting down")
			return
		}
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusServiceUnavailable, "queue_saturated", "Job queue is at capacity")
		return
	}
	writeJSON(w, http.StatusAccepted, job.ToResponse())
}

func validateJobBatch(requests []JobRequest) error {
	if len(requests) == 0 {
		return fmt.Errorf("requests is required")
	}
	seenIDs := make(map[string]struct{}, len(requests))
	for index, request := range requests {
		if strings.TrimSpace(request.ID) == "" {
			return fmt.Errorf("requests[%d].id is required", index)
		}
		if _, exists := seenIDs[request.ID]; exists {
			return fmt.Errorf("requests[%d].id must be unique", index)
		}
		seenIDs[request.ID] = struct{}{}
		if len(request.Messages) == 0 {
			return fmt.Errorf("requests[%d].messages is required", index)
		}
		for messageIndex, message := range request.Messages {
			if strings.TrimSpace(message.Content) == "" && !(message.Role == "assistant" && len(message.ToolCalls) > 0) {
				return fmt.Errorf("requests[%d].messages[%d].content is required", index, messageIndex)
			}
		}
		if _, err := structuredRequest(request.chatRequest()); err != nil {
			return fmt.Errorf("requests[%d].%w", index, err)
		}
	}
	return nil
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	if jobID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Job ID is required")
		return
	}
	job := s.jobManager.GetJob(jobID)
	if job == nil {
		writeError(w, http.StatusNotFound, "not_found", "Job not found")
		return
	}
	writeJSON(w, http.StatusOK, job.ToResponse())
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	if jobID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Job ID is required")
		return
	}
	job, status := s.jobManager.CancelJobStatus(jobID)
	if status == http.StatusNotFound {
		writeError(w, status, "not_found", "Job not found")
		return
	}
	if status != http.StatusOK {
		writeError(w, status, "invalid_request", "Job is no longer active")
		return
	}
	writeJSON(w, http.StatusOK, job.ToResponse())
}
