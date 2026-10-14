package evals

import (
	"encoding/json"
	"fmt"
)

func (s *WritingRunStore) loadWritingIntent(data []byte) error {
	var intent WritingDispatchIntent
	if err := json.Unmarshal(data, &intent); err != nil {
		return err
	}
	if err := validateWritingDispatchIntent(intent); err != nil {
		return err
	}
	key := writingIntentKey(intent.Kind, intent.Key, intent.Attempt)
	if _, exists := s.intents[key]; exists {
		return fmt.Errorf("duplicate dispatch intent %s", key)
	}
	s.intents[key] = intent
	return nil
}

func (s *WritingRunStore) loadWritingGeneration(data []byte) error {
	var record WritingGenerationRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return err
	}
	s.generations = append(s.generations, record)
	if record.Attempt > s.generationAttempts[record.Key] {
		s.generationAttempts[record.Key] = record.Attempt
	}
	if record.Status == success1 {
		s.generationSuccess[record.Key] = record
	}
	return nil
}

func (s *WritingRunStore) loadWritingJudgment(data []byte) error {
	var record WritingJudgmentRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return err
	}
	s.judgments = append(s.judgments, record)
	if record.Attempt > s.judgmentAttempts[record.Key] {
		s.judgmentAttempts[record.Key] = record.Attempt
	}
	if record.Status == success1 {
		s.judgmentSuccess[record.Key] = record
	}
	return nil
}

func (s *WritingRunStore) hasWritingGenerationResult(intent WritingDispatchIntent) bool {
	for _, record := range s.generations {
		if record.Key == intent.Key && record.Attempt == intent.Attempt {
			return true
		}
	}
	return false
}

func (s *WritingRunStore) hasWritingJudgmentResult(intent WritingDispatchIntent) bool {
	for _, record := range s.judgments {
		if record.Key == intent.Key && record.Attempt == intent.Attempt {
			return true
		}
	}
	return false
}
