package handler

import (
	"encoding/json"
	"testing"

	"github.com/openai/openai-go"
)

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("not json: %v\n%s", err, b)
	}
	return m
}

func TestAnOrdinaryAnswerCarriesNoAudioOrFunctionCall(t *testing.T) {
	resp := &openai.ChatCompletion{
		ID: "r1", Model: "m",
		Choices: []openai.ChatCompletionChoice{{
			FinishReason: "stop",
			Message:      openai.ChatCompletionMessage{Role: "assistant", Content: "好"},
		}},
	}
	body, err := openaiShape(resp)
	if err != nil {
		t.Fatal(err)
	}
	msg := decode(t, body)["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	for _, key := range []string{"audio", "function_call", "annotations", "tool_calls", "refusal"} {
		if _, ok := msg[key]; ok {
			t.Errorf("message still carries %q: %v", key, msg)
		}
	}
	if msg["content"] != "好" || msg["role"] != "assistant" {
		t.Errorf("content lost: %v", msg)
	}
}

func TestToolCallsAndRealAudioAreKept(t *testing.T) {
	resp := &openai.ChatCompletion{
		Choices: []openai.ChatCompletionChoice{{
			Message: openai.ChatCompletionMessage{
				Role: "assistant",
				ToolCalls: []openai.ChatCompletionMessageToolCall{{
					ID: "c1", Function: openai.ChatCompletionMessageToolCallFunction{Name: "f", Arguments: "{}"},
				}},
				Audio: openai.ChatCompletionAudio{ID: "a1", Data: "AAAA"},
			},
		}},
	}
	body, _ := openaiShape(resp)
	msg := decode(t, body)["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if _, ok := msg["tool_calls"]; !ok {
		t.Errorf("tool_calls dropped: %v", msg)
	}
	if _, ok := msg["audio"]; !ok {
		t.Errorf("a real audio answer was dropped: %v", msg)
	}
}

func TestAStreamedDeltaIsTidiedToo(t *testing.T) {
	chunk := openai.ChatCompletionChunk{
		ID: "c", Model: "m",
		Choices: []openai.ChatCompletionChunkChoice{{
			Delta: openai.ChatCompletionChunkChoiceDelta{Role: "assistant", Content: "好"},
		}},
	}
	body, err := openaiShape(chunk)
	if err != nil {
		t.Fatal(err)
	}
	delta := decode(t, body)["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)
	for _, key := range []string{"function_call", "tool_calls", "refusal"} {
		if _, ok := delta[key]; ok {
			t.Errorf("delta still carries %q: %v", key, delta)
		}
	}
	if delta["content"] != "好" {
		t.Errorf("content lost: %v", delta)
	}
}

func TestAnEmptyServiceTierIsDropped(t *testing.T) {
	body, _ := openaiShape(&openai.ChatCompletion{ID: "x"})
	if _, ok := decode(t, body)["service_tier"]; ok {
		t.Error("an empty service_tier would fail a client that validates it against OpenAI's list")
	}
}
