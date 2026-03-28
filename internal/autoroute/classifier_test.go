package autoroute

import (
	"testing"

	pkgopenai "github.com/openmux/openmux/pkg/openai"
)

func TestClassifyByRules(t *testing.T) {
	tests := []struct {
		name            string
		req             pkgopenai.ChatCompletionRequest
		expectedTier    Tier
		expectedCertain bool
	}{
		{
			name: "simple greeting → lite (certain)",
			req: pkgopenai.ChatCompletionRequest{
				Messages: []pkgopenai.ChatMessage{
					{Role: "user", Content: "你好"},
				},
			},
			expectedTier:    TierLite,
			expectedCertain: true,
		},
		{
			name: "short question → lite (certain)",
			req: pkgopenai.ChatCompletionRequest{
				Messages: []pkgopenai.ChatMessage{
					{Role: "user", Content: "Hi there"},
				},
			},
			expectedTier:    TierLite,
			expectedCertain: true,
		},
		{
			name: "code generation → large (certain)",
			req: pkgopenai.ChatCompletionRequest{
				Messages: []pkgopenai.ChatMessage{
					{Role: "user", Content: "帮我写一个快速排序的算法"},
				},
			},
			expectedTier:    TierLarge,
			expectedCertain: true,
		},
		{
			name: "math problem → reasoning (certain)",
			req: pkgopenai.ChatCompletionRequest{
				Messages: []pkgopenai.ChatMessage{
					{Role: "user", Content: "请一步一步推导勾股定理的证明"},
				},
			},
			expectedTier:    TierReasoning,
			expectedCertain: true,
		},
		{
			name: "debugging → reasoning (certain)",
			req: pkgopenai.ChatCompletionRequest{
				Messages: []pkgopenai.ChatMessage{
					{Role: "user", Content: "这段代码报错了，帮我debug一下"},
				},
			},
			expectedTier:    TierReasoning,
			expectedCertain: true,
		},
		{
			name: "function calling → large (certain)",
			req: pkgopenai.ChatCompletionRequest{
				Messages: []pkgopenai.ChatMessage{
					{Role: "user", Content: "查一下北京的天气"},
				},
				Tools: []pkgopenai.Tool{
					{Type: "function", Function: pkgopenai.FunctionDef{Name: "get_weather"}},
				},
			},
			expectedTier:    TierLarge,
			expectedCertain: true,
		},
		{
			name: "long conversation → large (certain)",
			req: pkgopenai.ChatCompletionRequest{
				Messages: func() []pkgopenai.ChatMessage {
					msgs := make([]pkgopenai.ChatMessage, 12)
					for i := range msgs {
						msgs[i] = pkgopenai.ChatMessage{Role: "user", Content: "消息内容"}
					}
					return msgs
				}(),
			},
			expectedTier:    TierLarge,
			expectedCertain: true,
		},
		{
			name: "ambiguous medium question → standard (uncertain)",
			req: pkgopenai.ChatCompletionRequest{
				Messages: []pkgopenai.ChatMessage{
					{Role: "user", Content: "能不能帮我解释一下机器学习和深度学习的区别？我想了解一下这个领域的基本概念"},
				},
			},
			expectedTier:    TierStandard,
			expectedCertain: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tier, certain := ClassifyByRules(&tt.req)
			if tier != tt.expectedTier {
				t.Errorf("tier = %v, want %v", tier, tt.expectedTier)
			}
			if certain != tt.expectedCertain {
				t.Errorf("certain = %v, want %v", certain, tt.expectedCertain)
			}
		})
	}
}

func TestParseTier(t *testing.T) {
	tests := []struct {
		input    string
		expected Tier
		ok       bool
	}{
		{"lite", TierLite, true},
		{"LARGE", TierLarge, true},
		{" Reasoning ", TierReasoning, true},
		{"standard", TierStandard, true},
		{"unknown", "", false},
	}

	for _, tt := range tests {
		tier, ok := parseTier(tt.input)
		if tier != tt.expected || ok != tt.ok {
			t.Errorf("parseTier(%q) = (%v, %v), want (%v, %v)", tt.input, tier, ok, tt.expected, tt.ok)
		}
	}
}
