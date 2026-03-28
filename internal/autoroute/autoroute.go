package autoroute

import (
	"context"
	"time"

	"github.com/openmux/openmux/internal/config"
	"github.com/openmux/openmux/internal/provider"
	"github.com/openmux/openmux/internal/router"
	"github.com/openmux/openmux/pkg/logger"
	pkgopenai "github.com/openmux/openmux/pkg/openai"
)

// AutoRouter 智能路由器
// 通过 discovery 自动生成的 auto:lite/standard/large/reasoning 路由
// 根据请求复杂度选择合适的层级
type AutoRouter struct {
	alias  string
	router *router.Router

	// 可选：LLM 分类器
	classifierProvider string
	classifierModel    string
	classifierAPIKey   string
	providerPool       *provider.Pool

	// 可选：手动 tier→route 覆盖
	tierOverrides map[Tier]string
}

// New 创建 AutoRouter
func New(cfg config.AutoRouteConfig, allCfg *config.Config, pool *provider.Pool, r *router.Router) *AutoRouter {
	alias := cfg.Alias
	if alias == "" {
		alias = "auto"
	}

	ar := &AutoRouter{
		alias:         alias,
		router:        r,
		providerPool:  pool,
		tierOverrides: make(map[Tier]string),
	}

	// 手动覆盖（如果用户显式配置了）
	if cfg.Lite != "" {
		ar.tierOverrides[TierLite] = cfg.Lite
	}
	if cfg.Standard != "" {
		ar.tierOverrides[TierStandard] = cfg.Standard
	}
	if cfg.Large != "" {
		ar.tierOverrides[TierLarge] = cfg.Large
	}
	if cfg.Reasoning != "" {
		ar.tierOverrides[TierReasoning] = cfg.Reasoning
	}

	// LLM 分类器
	if cfg.Classifier != "" {
		parts := splitProviderModel(cfg.Classifier)
		if len(parts) == 2 {
			ar.classifierProvider = parts[0]
			ar.classifierModel = parts[1]
			if provCfg, ok := allCfg.Providers[ar.classifierProvider]; ok && len(provCfg.APIKeys) > 0 {
				ar.classifierAPIKey = provCfg.APIKeys[0]
			}
			logger.Infof("AutoRoute: LLM classifier: %s/%s", ar.classifierProvider, ar.classifierModel)
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

func (a *AutoRouter) Alias() string {
	return a.alias
}

// Resolve 根据请求内容选择合适的路由
func (a *AutoRouter) Resolve(ctx context.Context, req *pkgopenai.ChatCompletionRequest) string {
	// 阶段1: 规则快速分类
	tier, certain := ClassifyByRules(req)
	if !certain && a.classifierProvider != "" && a.providerPool != nil {
		// 阶段2: LLM 分类
		if llmTier := a.classifyByLLM(ctx, req); llmTier != "" {
			tier = llmTier
		}
	}

	routeName := a.resolveRoute(tier)
	logger.Infof("AutoRoute: %s → %q", tier, routeName)
	return routeName
}

// resolveRoute 查找 tier 对应的路由
// 优先级：手动覆盖 > discovery 生成的 auto:* > 降级到其他 tier
func (a *AutoRouter) resolveRoute(tier Tier) string {
	// 1. 检查手动覆盖
	if name, ok := a.tierOverrides[tier]; ok {
		if _, err := a.router.Route(name); err == nil {
			return name
		}
	}

	// 2. 检查 discovery 自动生成的路由 (auto:lite, auto:standard, etc.)
	autoRoute := "auto:" + string(tier)
	if _, err := a.router.Route(autoRoute); err == nil {
		return autoRoute
	}

	// 3. Fallback: 尝试相邻 tier
	fallbacks := map[Tier][]Tier{
		TierLite:      {TierStandard, TierLarge},
		TierStandard:  {TierLarge, TierLite},
		TierLarge:     {TierStandard, TierReasoning},
		TierReasoning: {TierLarge, TierStandard},
	}

	for _, fb := range fallbacks[tier] {
		if name, ok := a.tierOverrides[fb]; ok {
			if _, err := a.router.Route(name); err == nil {
				return name
			}
		}
		fbRoute := "auto:" + string(fb)
		if _, err := a.router.Route(fbRoute); err == nil {
			return fbRoute
		}
	}

	return ""
}

func (a *AutoRouter) classifyByLLM(ctx context.Context, req *pkgopenai.ChatCompletionRequest) Tier {
	prov, err := a.providerPool.Get(a.classifierProvider)
	if err != nil {
		return ""
	}

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
		logger.Warnf("AutoRoute: LLM classify failed: %v", err)
		return ""
	}

	if len(resp.Choices) == 0 {
		return ""
	}

	tier, ok := parseTier(resp.Choices[0].Message.Content)
	if !ok {
		return ""
	}

	logger.Infof("AutoRoute: LLM → %s", tier)
	return tier
}
