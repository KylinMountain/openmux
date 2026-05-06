package discovery

import "testing"

func TestParseModelSize(t *testing.T) {
	tests := []struct {
		modelID  string
		expected float64
	}{
		{"meta-llama/llama-3.3-70b-instruct:free", 70},
		{"google/gemma-3-4b-it:free", 4},
		{"nousresearch/hermes-3-llama-3.1-405b:free", 405},
		{"nvidia/nemotron-3-super-120b-a12b:free", 12},  // MoE active
		{"qwen/qwen3-next-80b-a3b-instruct:free", 3},    // MoE active
		{"Qwen/Qwen2.5-7B-Instruct", 7},
		{"Qwen/Qwen2.5-72B-Instruct", 72},
		{"deepseek-ai/DeepSeek-V3", 0},                   // no size in name
		{"liquid/lfm-2.5-1.2b-instruct:free", 1.2},
		{"mistralai/mistral-small-3.1-24b-instruct:free", 24},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			size := ParseModelSize(tt.modelID)
			if size != tt.expected {
				t.Errorf("ParseModelSize(%q) = %v, want %v", tt.modelID, size, tt.expected)
			}
		})
	}
}

func TestParseModelCapability(t *testing.T) {
	tests := []struct {
		modelID  string
		expected ModelCapability
	}{
		{"liquid/lfm-2.5-1.2b-thinking:free", CapReasoning},
		{"deepseek-ai/DeepSeek-R1", CapReasoning},
		{"Qwen/QwQ-32B", CapReasoning},
		{"meta-llama/llama-3.3-70b-instruct:free", CapChat},
		{"google/gemma-3-4b-it:free", CapChat},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			cap := ParseModelCapability(tt.modelID)
			if cap != tt.expected {
				t.Errorf("ParseModelCapability(%q) = %v, want %v", tt.modelID, cap, tt.expected)
			}
		})
	}
}

func TestClassifyTier(t *testing.T) {
	tests := []struct {
		size     float64
		cap      ModelCapability
		expected ModelTier
	}{
		{4, CapChat, TierLite},
		{7, CapChat, TierLite},
		{24, CapChat, TierStandard},
		{70, CapChat, TierStandard},
		{405, CapChat, TierLarge},
		{120, CapChat, TierLarge},
		{7, CapReasoning, TierReasoning},
		{0, CapChat, TierStandard},
	}

	for _, tt := range tests {
		tier := ClassifyTier(tt.size, tt.cap)
		if tier != tt.expected {
			t.Errorf("ClassifyTier(%v, %v) = %v, want %v", tt.size, tt.cap, tier, tt.expected)
		}
	}
}
