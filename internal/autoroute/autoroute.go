package autoroute

import (
	"github.com/openmux/openmux/internal/config"
	"github.com/openmux/openmux/pkg/logger"
	pkgopenai "github.com/openmux/openmux/pkg/openai"
)

// AutoRouter 智能路由器，根据请求复杂度选择模型层级
type AutoRouter struct {
	alias    string
	tierMap  map[Tier]string // tier → model route name
}

// New 创建 AutoRouter
func New(cfg config.AutoRouteConfig) *AutoRouter {
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

	return &AutoRouter{
		alias:   alias,
		tierMap: tierMap,
	}
}

// Alias 返回触发别名
func (a *AutoRouter) Alias() string {
	return a.alias
}

// Resolve 根据请求内容解析出实际的模型路由名
// 返回空字符串表示没有配置对应 tier
func (a *AutoRouter) Resolve(req *pkgopenai.ChatCompletionRequest) string {
	tier := Classify(req)
	routeName := a.resolve(tier)
	logger.Infof("AutoRoute: classified as %s → route %q", tier, routeName)
	return routeName
}

// resolve 查找 tier 对应的路由名，如果没有则 fallback 到更低层级
func (a *AutoRouter) resolve(tier Tier) string {
	// 按优先级尝试：当前 tier → 降级
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

	// 最后兜底：返回任意一个配置了的 tier
	for _, name := range a.tierMap {
		return name
	}
	return ""
}
