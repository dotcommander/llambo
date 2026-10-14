package evals

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

func setMTPEnabled(data []byte, parent string, enabled bool) ([]byte, error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse model settings: %w", err)
	}
	if root == nil {
		return nil, errors.New("parse model settings: root object is required")
	}
	if _, ok := root[parent]; ok {
		if err := replaceMTPModelObject(root, parent, enabled); err != nil {
			return nil, err
		}
		return json.Marshal(root)
	}
	rawModels, ok := root["models"]
	if !ok {
		return nil, fmt.Errorf("model settings do not contain parent %q", parent)
	}
	updated, err := updateMTPModelContainer(rawModels, parent, enabled)
	if err != nil {
		return nil, err
	}
	root["models"] = updated
	return json.Marshal(root)
}

func replaceMTPModelObject(models map[string]json.RawMessage, parent string, enabled bool) error {
	raw, found := models[parent]
	if !found {
		return fmt.Errorf("model settings do not contain parent %q", parent)
	}
	updated, err := setMTPInModelObject(raw, enabled)
	if err != nil {
		return fmt.Errorf("update model %q: %w", parent, err)
	}
	models[parent] = updated
	return nil
}

func updateMTPModelContainer(raw json.RawMessage, parent string, enabled bool) (json.RawMessage, error) {
	var byID map[string]json.RawMessage
	if json.Unmarshal(raw, &byID) == nil && byID != nil {
		if err := replaceMTPModelObject(byID, parent, enabled); err != nil {
			return nil, err
		}
		return json.Marshal(byID)
	}
	return updateMTPModelList(raw, parent, enabled)
}

func updateMTPModelList(rawModels json.RawMessage, parent string, enabled bool) (json.RawMessage, error) {
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(rawModels, &entries); err != nil {
		return nil, fmt.Errorf("parse model settings list: %w", err)
	}
	found := 0
	for _, entry := range entries {
		id := mtpSettingsModelID(entry)
		if id != parent {
			continue
		}
		found++
		entry["vlm_mtp_enabled"] = json.RawMessage(strconv.FormatBool(enabled))
	}
	if found != 1 {
		return nil, fmt.Errorf("model settings contain %d entries for parent %q", found, parent)
	}

	return json.Marshal(entries)
}

func mtpSettingsModelID(entry map[string]json.RawMessage) string {
	id := ""
	for _, key := range []string{"id", "model", "name"} {
		if raw := entry[key]; raw != nil {
			_ = json.Unmarshal(raw, &id)
			if id != "" {
				break
			}
		}
	}
	return id
}

func setMTPInModelObject(raw json.RawMessage, enabled bool) (json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("parse model settings object: %w", err)
	}
	if object == nil {
		return nil, errors.New("model settings must be an object")
	}
	object["vlm_mtp_enabled"] = json.RawMessage(strconv.FormatBool(enabled))
	updated, err := json.Marshal(object)
	return updated, err
}
