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
var sizePattern = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*b`)

// MoE active params: "120b-a12b" → 提取 active params
var moeActivePattern = regexp.MustCompile(`(?i)(\d+)b[_-]a(\d+)b`)

// ParseModelSize 从模型 ID 解析参数量（单位: 十亿）
// 返回 0 表示无法解析
func ParseModelSize(modelID string) float64 {
	lower := strings.ToLower(modelID)

	// MoE 模型: 优先用 active params (如 "120b-a12b" → 12)
	if matches := moeActivePattern.FindStringSubmatch(lower); len(matches) == 3 {
		if active, err := strconv.ParseFloat(matches[2], 64); err == nil {
			return active
		}
	}

	// 从模型名的各段中找参数量
	// 优先匹配最后一个数字+b（通常是参数量）
	allMatches := sizePattern.FindAllStringSubmatch(lower, -1)
	if len(allMatches) > 0 {
		// 取最后一个匹配（通常 "qwen/qwen2.5-7b-instruct" 中 7b 是参数量）
		lastMatch := allMatches[len(allMatches)-1]
		if size, err := strconv.ParseFloat(lastMatch[1], 64); err == nil {
			return size
		}
	}

	return 0
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
