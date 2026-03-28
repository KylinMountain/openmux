package autoroute

import (
	"context"
	"time"

	"github.com/openmux/openmux/internal/config"
	"github.com/openmux/openmux/internal/provider"
	"github.com/openmux/openmux/pkg/logger"
	pkgopenai "github.com/openmux/openmux/pkg/openai"
)

// AutoRouter 智能路由器，根据请求复杂度选择模型层级
type AutoRouter struct {
	alias    string
	tierMap  map[Tier]string // tier → model route name
	// LLM 分类所需的配置
	classifierProvider string // 用于分类的 provider 名称
	classifierModel    string // 用于分类的 model 名称
	classifierAPIKey   string // API key
	providerPool       *provider.Pool
}

// New 创建 AutoRouter
func New(cfg config.AutoRouteConfig, allCfg *config.Config, pool *provider.Pool) *AutoRouter {
	alias := cfg.Alias
	if alias == "" {
		alias = "auto"
	}

	tierMap := map[Tier]string{}
	if cfg.Lite != "" {
		tierMap[TierLite] = cfg.Lite
	}
	if cfg.Standard != "" {
		tierMap[TierStandard] = cfg.Standard
	}
	if cfg.Large != "" {
		tierMap[TierLarge] = cfg.Large
	}
	if cfg.Reasoning != "" {
		tierMap[TierReasoning] = cfg.Reasoning
	}

	ar := &AutoRouter{
		alias:        alias,
		tierMap:       tierMap,
		providerPool: pool,
	}

	// 解析分类器模型配置 (格式: provider/model)
	if cfg.Classifier != "" {
		parts := splitProviderModel(cfg.Classifier)
		if len(parts) == 2 {
			ar.classifierProvider = parts[0]
			ar.classifierModel = parts[1]
			if provCfg, ok := allCfg.Providers[ar.classifierProvider]; ok && len(provCfg.APIKeys) > 0 {
				ar.classifierAPIKey = provCfg.APIKeys[0]
			}
			logger.Infof("AutoRoute: LLM classifier enabled (%s/%s)", ar.classifierProvider, ar.classifierModel)
		}
	}

	return ar
}

func splitProviderModel(s string) []string {
	for i, c := range s {
		if c == '/' || c == ':' {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}

// Alias 返回触发别名
func (a *AutoRouter) Alias() string {
	return a.alias
}

// Resolve 根据请求内容解析出实际的模型路由名
func (a *AutoRouter) Resolve(ctx context.Context, req *pkgopenai.ChatCompletionRequest) string {
	// 第一步：规则快速分类
	tier, certain := ClassifyByRules(req)
	if certain {
		routeName := a.resolveTier(tier)
		logger.Infof("AutoRoute: rules → %s → %q", tier, routeName)
		return routeName
	}

	// 第二步：如果配置了 LLM 分类器且规则不确定，用小模型判断
	if a.classifierProvider != "" && a.providerPool != nil {
		if llmTier := a.classifyByLLM(ctx, req); llmTier != "" {
			tier = llmTier
		}
	}

	routeName := a.resolveTier(tier)
	logger.Infof("AutoRoute: %s → %q", tier, routeName)
	return routeName
}

// classifyByLLM 使用小模型分类
func (a *AutoRouter) classifyByLLM(ctx context.Context, req *pkgopenai.ChatCompletionRequest) Tier {
	prov, err := a.providerPool.Get(a.classifierProvider)
	if err != nil {
		logger.Warnf("AutoRoute: classifier provider %q not found: %v", a.classifierProvider, err)
		return ""
	}

	// 用超短超时，分类不应该超过 5 秒
	classifyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	prompt := classificationPrompt(req.Messages)
	maxTokens := 10
	temperature := float32(0)
	classifyReq := &pkgopenai.ChatCompletionRequest{
		Model: a.classifierModel,
		Messages: []pkgopenai.ChatMessage{
			{Role: "user", Content: prompt},
		},
		MaxTokens:   &maxTokens,
		Temperature: &temperature,
	}

	resp, err := prov.ChatCompletion(classifyCtx, classifyReq, a.classifierModel, a.classifierAPIKey)
	if err != nil {
		logger.Warnf("AutoRoute: LLM classification failed: %v", err)
		return ""
	}

	if len(resp.Choices) == 0 {
		return ""
	}

	reply := resp.Choices[0].Message.Content
	tier, ok := parseTier(reply)
	if !ok {
		logger.Warnf("AutoRoute: LLM returned unexpected tier %q", reply)
		return ""
	}

	logger.Infof("AutoRoute: LLM classified as %s", tier)
	return tier
}

// resolveTier 查找 tier 对应的路由名，如果没有则 fallback 到相邻层级
func (a *AutoRouter) resolveTier(tier Tier) string {
	fallbackOrder := map[Tier][]Tier{
		TierLite:      {TierLite, TierStandard, TierLarge},
		TierStandard:  {TierStandard, TierLarge, TierLite},
		TierLarge:     {TierLarge, TierStandard, TierReasoning},
		TierReasoning: {TierReasoning, TierLarge, TierStandard},
	}

	for _, t := range fallbackOrder[tier] {
		if name, ok := a.tierMap[t]; ok {
			return name
		}
	}

	for _, name := range a.tierMap {
		return name
	}
	return ""
}
