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

// Classify 根据请求内容判断复杂度，返回适合的模型层级
func Classify(req *pkgopenai.ChatCompletionRequest) Tier {
	// 有 tools/function calling → 至少 large
	if len(req.Tools) > 0 {
		return TierLarge
	}

	// 计算总内容长度和分析最后一条用户消息
	totalLen := 0
	var lastUserMsg string
	for _, msg := range req.Messages {
		totalLen += utf8.RuneCountInString(msg.Content)
		if msg.Role == "user" {
			lastUserMsg = msg.Content
		}
	}

	msgLower := strings.ToLower(lastUserMsg)

	// 推理关键词检测
	if containsAny(msgLower, reasoningKeywords) {
		return TierReasoning
	}

	// 代码/复杂任务关键词
	if containsAny(msgLower, complexKeywords) {
		return TierLarge
	}

	// 结构化输出
	if req.ResponseFormat != nil && req.ResponseFormat.Type == "json_object" {
		return TierStandard
	}

	// 按内容长度分级
	turnCount := len(req.Messages)
	switch {
	case totalLen > 4000 || turnCount > 10:
		return TierLarge
	case totalLen > 500 || turnCount > 4:
		return TierStandard
	default:
		return TierLite
	}
}

var reasoningKeywords = []string{
	// 数学/逻辑
	"证明", "推导", "求解", "计算", "公式",
	"prove", "derive", "solve", "equation",
	// 推理
	"一步一步", "step by step", "think through",
	"逻辑", "推理", "分析原因", "为什么会",
	"reasoning", "logic",
	// 代码调试
	"debug", "调试", "bug", "报错", "error",
	"找出问题", "什么原因",
}

var complexKeywords = []string{
	// 代码
	"写代码", "写一个", "实现", "代码", "编程", "函数", "算法",
	"code", "implement", "function", "algorithm", "program",
	"refactor", "重构", "优化",
	// 长文写作
	"写一篇", "文章", "论文", "报告", "essay", "article", "report",
	// 深度分析
	"对比分析", "详细解释", "深入", "全面",
	"compare", "analyze", "explain in detail", "comprehensive",
}

func containsAny(text string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}
