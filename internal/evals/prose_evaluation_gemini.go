package evals

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// DefaultProseEvaluationMaxTokens leaves room for six evidence-backed
// dimensions while keeping the response bounded.
const DefaultProseEvaluationMaxTokens = 8192

var geminiUnsupportedSchemaKeywords = map[string]struct{}{
	"$id":                  {},
	"$schema":              {},
	"$defs":                {},
	"additionalProperties": {},
	"definitions":          {},
}

func projectGeminiProseSchema(schema json.RawMessage) ([]byte, error) {
	trimmed := bytes.TrimSpace(schema)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("project prose schema for Gemini: schema is empty")
	}
	if err := rejectDuplicateProseJSONKeys(trimmed); err != nil {
		return nil, fmt.Errorf("project prose schema for Gemini: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	var root any
	if err := decoder.Decode(&root); err != nil {
		return nil, fmt.Errorf("project prose schema for Gemini: parse schema: %w", err)
	}
	if _, ok := root.(map[string]any); !ok {
		return nil, fmt.Errorf("project prose schema for Gemini: root must be an object")
	}
	projected, err := projectGeminiProseSchemaNode(root, root, make(map[string]bool))
	if err != nil {
		return nil, fmt.Errorf("project prose schema for Gemini: %w", err)
	}
	result, err := json.Marshal(projected)
	if err != nil {
		return nil, fmt.Errorf("project prose schema for Gemini: encode schema: %w", err)
	}
	return result, nil
}

func projectGeminiProseSchemaNode(node, root any, resolving map[string]bool) (any, error) {
	switch value := node.(type) {
	case map[string]any:
		if rawReference, ok := value["$ref"]; ok {
			reference, ok := rawReference.(string)
			if !ok || strings.TrimSpace(reference) == "" {
				return nil, fmt.Errorf("$ref must be a non-empty string")
			}
			if len(value) != 1 {
				return nil, fmt.Errorf("$ref %q must not have sibling keywords", reference)
			}
			if resolving[reference] {
				return nil, fmt.Errorf("cyclic $ref %q", reference)
			}
			target, err := resolveLocalSchemaReference(root, reference)
			if err != nil {
				return nil, err
			}
			resolving[reference] = true
			projected, err := projectGeminiProseSchemaNode(target, root, resolving)
			delete(resolving, reference)
			return projected, err
		}

		projected := make(map[string]any, len(value))
		for key, child := range value {
			if _, unsupported := geminiUnsupportedSchemaKeywords[key]; unsupported {
				continue
			}
			projectedChild, err := projectGeminiProseSchemaNode(child, root, resolving)
			if err != nil {
				return nil, err
			}
			projected[key] = projectedChild
		}
		return projected, nil
	case []any:
		projected := make([]any, len(value))
		for index, child := range value {
			projectedChild, err := projectGeminiProseSchemaNode(child, root, resolving)
			if err != nil {
				return nil, err
			}
			projected[index] = projectedChild
		}
		return projected, nil
	default:
		return value, nil
	}
}

func resolveLocalSchemaReference(root any, reference string) (any, error) {
	if !strings.HasPrefix(reference, "#/") {
		return nil, fmt.Errorf("unsupported non-local $ref %q", reference)
	}
	current := root
	for _, encodedToken := range strings.Split(strings.TrimPrefix(reference, "#/"), "/") {
		token, err := decodeJSONPointerToken(encodedToken)
		if err != nil {
			return nil, fmt.Errorf("invalid $ref %q: %w", reference, err)
		}
		switch value := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = value[token]
			if !ok {
				return nil, fmt.Errorf("unresolved $ref %q", reference)
			}
		case []any:
			index, err := strconv.Atoi(token)
			if err != nil || index < 0 || index >= len(value) {
				return nil, fmt.Errorf("unresolved $ref %q", reference)
			}
			current = value[index]
		default:
			return nil, fmt.Errorf("unresolved $ref %q", reference)
		}
	}
	return current, nil
}

func decodeJSONPointerToken(encoded string) (string, error) {
	var decoded strings.Builder
	for index := 0; index < len(encoded); index++ {
		if encoded[index] != '~' {
			decoded.WriteByte(encoded[index])
			continue
		}
		if index+1 >= len(encoded) {
			return "", fmt.Errorf("trailing JSON Pointer escape")
		}
		index++
		switch encoded[index] {
		case '0':
			decoded.WriteByte('~')
		case '1':
			decoded.WriteByte('/')
		default:
			return "", fmt.Errorf("unsupported JSON Pointer escape ~%c", encoded[index])
		}
	}
	return decoded.String(), nil
}
