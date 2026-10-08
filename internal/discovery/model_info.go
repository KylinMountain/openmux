package discovery

import (
	"regexp"
	"strconv"
	"strings"
)

// ModelCapability 模型能力类型
type ModelCapability string

const (
	CapChat      ModelCapability = "chat"
	CapReasoning ModelCapability = "reasoning"
)

// ModelTier 模型层级
type ModelTier string

const (
	TierLite      ModelTier = "lite"      // < 10B
	TierStandard  ModelTier = "standard"  // 10B ~ 70B
	TierLarge     ModelTier = "large"     // > 70B
	TierReasoning ModelTier = "reasoning" // 推理模型（任意大小）
)

// 参数量匹配: "70b", "405B", "7b", "1.5b", "120b-a12b" (MoE active params)
// 也支持万亿参数量: "1t", "1.8T" → 折算为 1000B / 1800B
var sizePattern = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*([bt])\b`)

// MoE active params: "120b-a12b" → 提取 active params
var moeActivePattern = regexp.MustCompile(`(?i)(\d+)b[_-]a(\d+)b`)

// ParseModelSize 从模型 ID 解析参数量（单位: 十亿）
// 接受多个候选字符串（如 model id、canonical_slug、hugging_face_id），依次尝试，
// 任意一个解析出非零结果即返回。返回 0 表示全部无法解析。
//
// 例: ParseModelSize("z-ai/glm-4.5-air:free", "z-ai/glm-4.5-air", "zai-org/GLM-4.5-Air") = 0
//     ParseModelSize("qwen/qwen3-coder:free", "qwen/qwen3-coder-480b-a35b-07-25", "Qwen/Qwen3-Coder-480B-A35B-Instruct") = 35 (MoE active)
func ParseModelSize(candidates ...string) float64 {
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if size := parseSizeOne(c); size > 0 {
			return size
		}
	}
	return 0
}

func parseSizeOne(s string) float64 {
	lower := strings.ToLower(s)

	// MoE 模型: 优先用 active params (如 "120b-a12b" → 12)
	if matches := moeActivePattern.FindStringSubmatch(lower); len(matches) == 3 {
		if active, err := strconv.ParseFloat(matches[2], 64); err == nil {
			return active
		}
	}

	// 从模型名的各段中找参数量
	// 优先匹配最后一个数字+b/t（通常是参数量；t 表示 trillion，按 1T = 1000B 折算）
	allMatches := sizePattern.FindAllStringSubmatch(lower, -1)
	if len(allMatches) > 0 {
		lastMatch := allMatches[len(allMatches)-1]
		size, err := strconv.ParseFloat(lastMatch[1], 64)
		if err != nil {
			return 0
		}
		if strings.ToLower(lastMatch[2]) == "t" {
			size *= 1000
		}
		return size
	}

	return 0
}

// IsTextGeneration 判断 OpenRouter modality 是否属于文本生成（聊天/补全）类。
//
// modality 形如 "text->text"、"text+image->text"、"text+image->text+audio"。
// 判定规则: 输出必须含 text，且不能含 audio / image / video（避免归入 free 聚合时
// 把语音、图像生成模型与普通 chat 模型混在一起）。
// 空字符串视为未知，按聊天模型对待（兼容其他 provider 不返回 modality 的情况）。
func IsTextGeneration(modality string) bool {
	m := strings.ToLower(strings.TrimSpace(modality))
	if m == "" {
		return true
	}
	parts := strings.SplitN(m, "->", 2)
	if len(parts) != 2 {
		return true
	}
	output := strings.TrimSpace(parts[1])
	if !strings.Contains(output, "text") {
		return false
	}
	for _, nonText := range []string{"audio", "image", "video"} {
		if strings.Contains(output, nonText) {
			return false
		}
	}
	return true
}

// ParseModelCapability 从模型 ID 判断能力类型
func ParseModelCapability(modelID string) ModelCapability {
	lower := strings.ToLower(modelID)

	reasoningKeywords := []string{
		"thinking", "reason", "r1", "o1", "o3",
		"z1", "qwq", "cot",
	}
	for _, kw := range reasoningKeywords {
		if strings.Contains(lower, kw) {
			return CapReasoning
		}
	}

	return CapChat
}

// ClassifyTier 根据参数量和能力分配层级
func ClassifyTier(sizeB float64, cap ModelCapability) ModelTier {
	if cap == CapReasoning {
		return TierReasoning
	}

	switch {
	case sizeB <= 0:
		return TierStandard // 无法判断大小，默认 standard
	case sizeB < 10:
		return TierLite
	case sizeB <= 72:
		return TierStandard
	default:
		return TierLarge
	}
}
