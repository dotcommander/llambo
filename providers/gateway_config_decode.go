package providers

import (
	"encoding/json"
	"fmt"
)

func (g *GatewayConfig) UnmarshalJSON(data []byte) error {
	type plain GatewayConfig
	var decoded plain
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	for _, name := range []string{"max_retained_jobs", "max_retained_payload_bytes", "handler_timeout_seconds", "write_timeout_seconds", "shutdown_timeout_seconds"} {
		if raw, ok := fields[name]; ok {
			var value int64
			if err := json.Unmarshal(raw, &value); err != nil {
				return err
			}
			if value <= 0 {
				return fmt.Errorf("gateway %s must be positive", name)
			}
		}
	}
	*g = GatewayConfig(decoded)
	return nil
}
