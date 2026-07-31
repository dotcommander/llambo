package gateway

import (
	"encoding/json"
	"fmt"
	"io"
)

// decodeSingleJSON accepts exactly one JSON value followed only by whitespace.
// OpenAI-compatible endpoints remain permissive about unknown fields; callers
// opt into strict field checks only where the Anthropic compatibility contract
// requires them.
func decodeSingleJSON(r io.Reader, dst any, strictUnknownFields bool) error {
	dec := json.NewDecoder(r)
	if strictUnknownFields {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("request body must contain a single JSON value")
		}
		return err
	}
	return nil
}
