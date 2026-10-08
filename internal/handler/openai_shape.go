package handler

import (
	"encoding/json"
)

// openaiShape re-encodes a chat completion (or chunk) the way OpenAI does: fields the SDK
// we proxy through always serialises, but OpenAI leaves out when they hold nothing, are dropped.
//
// The Go SDK's response structs have no omitempty, so an answer carried
// "audio":{"id":"","data":"",...} and "function_call":{"arguments":"","name":""}. Clients that
// test those for presence (the OpenAI Agents SDK raises "Audio is not currently supported"
// on any non-null audio) then refuse an ordinary text answer.
func openaiShape(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return raw, nil // not an object: nothing to tidy
	}
	for _, key := range []string{"service_tier", "system_fingerprint"} {
		if s, ok := doc[key].(string); ok && s == "" {
			delete(doc, key)
		}
	}
	choices, _ := doc["choices"].([]any)
	for _, c := range choices {
		choice, _ := c.(map[string]any)
		for _, part := range []string{"message", "delta"} {
			if m, ok := choice[part].(map[string]any); ok {
				dropEmpty(m)
			}
		}
	}
	return json.Marshal(doc)
}

// dropEmpty removes, from one message or delta, the fields that carry no content.
func dropEmpty(m map[string]any) {
	if a, ok := m["audio"].(map[string]any); ok && emptyAudio(a) {
		delete(m, "audio")
	}
	if f, ok := m["function_call"].(map[string]any); ok && f["name"] == "" && f["arguments"] == "" {
		delete(m, "function_call")
	}
	for _, key := range []string{"annotations", "tool_calls", "audio", "function_call"} {
		if m[key] == nil {
			delete(m, key)
		}
	}
	if s, ok := m["refusal"].(string); ok && s == "" {
		delete(m, "refusal")
	}
}

func emptyAudio(a map[string]any) bool {
	for _, key := range []string{"id", "data", "transcript"} {
		if s, _ := a[key].(string); s != "" {
			return false
		}
	}
	if n, _ := a["expires_at"].(float64); n != 0 {
		return false
	}
	return true
}
