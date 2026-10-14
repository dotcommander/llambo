package evals

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

func validateProseDimension(id string, v ProseDimensionEvaluation, prose string) error {
	if id == mechanics1 && !v.Applicable {
		return errors.New("mechanics must be applicable")
	}
	if v.Score < 1 || v.Score > 5 {
		return fmt.Errorf("%s score must be an integer from 1 through 5, got %d", id, v.Score)
	}
	if strings.TrimSpace(v.Rationale) != v.Rationale || len(v.Rationale) > 240 || !proseRationalePattern.MatchString(v.Rationale) {
		return fmt.Errorf("%s rationale must be one concise trimmed sentence", id)
	}
	if len(v.Evidence) == 0 {
		return fmt.Errorf("%s requires at least one evidence span", id)
	}
	for _, span := range v.Evidence {
		if span.Start < 0 || span.End <= span.Start || span.End > len(prose) || !utf8.ValidString(prose[span.Start:span.End]) {
			return fmt.Errorf("%s has an invalid evidence span [%d,%d)", id, span.Start, span.End)
		}
	}
	return nil
}

func walkProseJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		return walkProseJSONObject(decoder)
	case '[':
		return walkProseJSONArray(decoder)
	default:
		return fmt.Errorf("unexpected delimiter %q", delim)
	}
}

func walkProseJSONObject(decoder *json.Decoder) error {
	seen := map[string]struct{}{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := keyToken.(string)
		if !ok {
			return errors.New("object key is not a string")
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate field %q", key)
		}
		seen[key] = struct{}{}
		if err := walkProseJSONValue(decoder); err != nil {
			return err
		}
	}
	return closeProseJSONValue(decoder, '}', "object")
}

func walkProseJSONArray(decoder *json.Decoder) error {
	for decoder.More() {
		if err := walkProseJSONValue(decoder); err != nil {
			return err
		}
	}
	return closeProseJSONValue(decoder, ']', "array")
}

func closeProseJSONValue(decoder *json.Decoder, expected json.Delim, kind string) error {
	end, err := decoder.Token()
	if err != nil {
		return err
	}
	if end != expected {
		return fmt.Errorf("%s has invalid closing delimiter %q", kind, end)
	}
	return nil
}
