package autoroute

import (
	"strings"
	"unicode/utf8"

	pkgopenai "github.com/openmux/openmux/pkg/openai"
)

// Tier 模型层级
type Tier string

const (
	TierLite      Tier = "lite"      // 简单任务：打招呼、翻译短句、简单问答
	TierStandard  Tier = "standard"  // 常规任务：普通对话、摘要、一般写作
	TierLarge     Tier = "large"     // 复杂任务：长文写作、代码生成、深度分析
	TierReasoning Tier = "reasoning" // 推理任务：数学、逻辑推理、代码调试、多步骤规划
)

// parseTier 从字符串解析 Tier
func parseTier(s string) (Tier, bool) {
	switch strings.TrimSpace(strings.ToLower(s)) {
	case "lite":
		return TierLite, true
	case "standard":
		return TierStandard, true
	case "large":
		return TierLarge, true
	case "reasoning":
		return TierReasoning, true
	}
	return "", false
}

// ClassifyByRules 基于规则的快速分类（零延迟）
// 返回 tier 和是否确定（false 表示需要 LLM 进一步判断）
func ClassifyByRules(req *pkgopenai.ChatCompletionRequest) (Tier, bool) {
	// 有 tools/function calling → 确定是 large
	if len(req.Tools) > 0 {
		return TierLarge, true
	}

	totalLen := 0
	var lastUserMsg string
	for _, msg := range req.Messages {
		totalLen += utf8.RuneCountInString(msg.Content)
		if msg.Role == "user" {
			lastUserMsg = msg.Content
		}
	}

	msgLower := strings.ToLower(lastUserMsg)
	msgLen := utf8.RuneCountInString(lastUserMsg)

	// 明确的推理关键词 → 确定是 reasoning（优先于长度判断）
	if containsAny(msgLower, strongReasoningKeywords) {
		return TierReasoning, true
	}

	// 明确的代码/复杂关键词 → 确定是 large
	if containsAny(msgLower, strongComplexKeywords) {
		return TierLarge, true
	}

	// 极短消息且无关键词命中 → 确定是 lite
	if msgLen < 20 && len(req.Messages) <= 2 {
		return TierLite, true
	}

	// 超长内容 → 确定是 large
	if totalLen > 4000 || len(req.Messages) > 10 {
		return TierLarge, true
	}

	// 无法确定 → 需要 LLM 判断
	return TierStandard, false
}

// 强信号关键词 — 命中即确定
var strongReasoningKeywords = []string{
	"证明", "推导", "求解", "prove", "derive", "solve",
	"一步一步", "step by step", "think through",
	"debug", "调试",
}

var strongComplexKeywords = []string{
	"写代码", "实现", "代码", "编程", "算法",
	"code", "implement", "algorithm", "program",
	"写一篇", "论文", "essay", "article",
}

func containsAny(text string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}

// classificationPrompt 生成分类提示词
func classificationPrompt(userMessages []pkgopenai.ChatMessage) string {
	// 只取最后一条用户消息的前 500 字符
	var lastMsg string
	for _, msg := range userMessages {
		if msg.Role == "user" {
			lastMsg = msg.Content
		}
	}
	if utf8.RuneCountInString(lastMsg) > 500 {
		runes := []rune(lastMsg)
		lastMsg = string(runes[:500])
	}

	return `Classify this user request into exactly one category. Reply with ONLY the category name, nothing else.

Categories:
- lite: simple greetings, chitchat, short factual questions, translations
- standard: normal conversation, summaries, general writing, explanations
- large: code generation, long-form writing, deep analysis, complex tasks
- reasoning: math proofs, logic puzzles, debugging, multi-step problem solving

User request: ` + lastMsg
}
