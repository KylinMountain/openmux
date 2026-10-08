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
		{"inclusionai/ling-2.6-1t:free", 1000},        // 1T → 1000B
		{"some-vendor/big-model-1.8t-chat", 1800},     // 1.8T → 1800B
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

func TestParseModelSizeMultiSource(t *testing.T) {
	// id 解析不出，但 canonical_slug 带参数量（MoE active params）
	if size := ParseModelSize("qwen/qwen3-coder:free", "qwen/qwen3-coder-480b-a35b-07-25", "Qwen/Qwen3-Coder-480B-A35B-Instruct"); size != 35 {
		t.Errorf("qwen3-coder via canonical_slug: got %v, want 35 (MoE active)", size)
	}
	// id 与 slug 都没参数量，只能从 hf_id 取
	if size := ParseModelSize("z-ai/glm-4.5-air:free", "z-ai/glm-4.5-air", "zai-org/GLM-4.5-Air-110B"); size != 110 {
		t.Errorf("glm via hf_id: got %v, want 110", size)
	}
	// 全部为空字符串
	if size := ParseModelSize("", "", ""); size != 0 {
		t.Errorf("all empty: got %v, want 0", size)
	}
	// 第一个就解出非零值，应当短路返回
	if size := ParseModelSize("nousresearch/hermes-3-llama-3.1-405b:free", "irrelevant-1t", ""); size != 405 {
		t.Errorf("first hit short-circuit: got %v, want 405", size)
	}
}

func TestIsTextGeneration(t *testing.T) {
	tests := []struct {
		modality string
		want     bool
	}{
		{"text->text", true},
		{"text+image->text", true},
		{"text->audio", false},
		{"text->image", false},
		{"text+image->text+audio", false}, // lyria 类多模态生成
		{"text+image->text", true},        // OCR / VLM 输出文本
		{"", true}, // 未知按聊天模型对待
		{"weird-no-arrow", true},
	}
	for _, tt := range tests {
		if got := IsTextGeneration(tt.modality); got != tt.want {
			t.Errorf("IsTextGeneration(%q) = %v, want %v", tt.modality, got, tt.want)
		}
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
