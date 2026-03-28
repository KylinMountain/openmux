package autoroute

import (
	"testing"

	pkgopenai "github.com/openmux/openmux/pkg/openai"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name     string
		req      pkgopenai.ChatCompletionRequest
		expected Tier
	}{
		{
			name: "simple greeting → lite",
			req: pkgopenai.ChatCompletionRequest{
				Messages: []pkgopenai.ChatMessage{
					{Role: "user", Content: "你好"},
				},
			},
			expected: TierLite,
		},
		{
			name: "short question → lite",
			req: pkgopenai.ChatCompletionRequest{
				Messages: []pkgopenai.ChatMessage{
					{Role: "user", Content: "今天天气怎么样？"},
				},
			},
			expected: TierLite,
		},
		{
			name: "code generation → large",
			req: pkgopenai.ChatCompletionRequest{
				Messages: []pkgopenai.ChatMessage{
					{Role: "user", Content: "帮我写一个快速排序的算法"},
				},
			},
			expected: TierLarge,
		},
		{
			name: "math problem → reasoning",
			req: pkgopenai.ChatCompletionRequest{
				Messages: []pkgopenai.ChatMessage{
					{Role: "user", Content: "请一步一步推导勾股定理的证明"},
				},
			},
			expected: TierReasoning,
		},
		{
			name: "debugging → reasoning",
			req: pkgopenai.ChatCompletionRequest{
				Messages: []pkgopenai.ChatMessage{
					{Role: "user", Content: "这段代码报错了，帮我debug一下"},
				},
			},
			expected: TierReasoning,
		},
		{
			name: "function calling → large",
			req: pkgopenai.ChatCompletionRequest{
				Messages: []pkgopenai.ChatMessage{
					{Role: "user", Content: "查一下北京的天气"},
				},
				Tools: []pkgopenai.Tool{
					{Type: "function", Function: pkgopenai.FunctionDef{Name: "get_weather"}},
				},
			},
			expected: TierLarge,
		},
		{
			name: "long conversation → large",
			req: pkgopenai.ChatCompletionRequest{
				Messages: func() []pkgopenai.ChatMessage {
					msgs := make([]pkgopenai.ChatMessage, 12)
					for i := range msgs {
						msgs[i] = pkgopenai.ChatMessage{Role: "user", Content: "消息内容"}
					}
					return msgs
				}(),
			},
			expected: TierLarge,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tier := Classify(&tt.req)
			if tier != tt.expected {
				t.Errorf("Classify() = %v, want %v", tier, tt.expected)
			}
		})
	}
}
