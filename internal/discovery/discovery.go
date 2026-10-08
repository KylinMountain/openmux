package discovery

import (
	"context"
	"sort"
	"time"

	"github.com/openmux/openmux/internal/config"
	"github.com/openmux/openmux/internal/router"
	"github.com/openmux/openmux/pkg/logger"
)

// ModelDiscovery 模型自动发现服务
type ModelDiscovery struct {
	cfg    *config.Config
	router *router.Router
	cancel context.CancelFunc
	hf     *HFClient // 仅当 cfg.Discovery.FetchHuggingFaceMetadata 为 true 时初始化
}

func NewModelDiscovery(cfg *config.Config, r *router.Router) *ModelDiscovery {
	d := &ModelDiscovery{cfg: cfg, router: r}
	if cfg.Discovery.FetchHuggingFaceMetadata {
		d.hf = NewHFClient()
	}
	return d
}

func (d *ModelDiscovery) Start(ctx context.Context) {
	ctx, d.cancel = context.WithCancel(ctx)
	d.discoverOnce(ctx)

	interval := d.cfg.Discovery.Interval
	if interval <= 0 {
		interval = 24 * time.Hour
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Infof("Model discovery service stopped")
			return
		case <-ticker.C:
			d.discoverOnce(ctx)
		}
	}
}

func (d *ModelDiscovery) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
}

func (d *ModelDiscovery) discoverOnce(ctx context.Context) {
	logger.Infof("Starting model discovery...")

	allModels := make(map[string][]config.Target)
	// 待写入 router 的元信息（与 allModels 路由名一一对应）
	allMeta := make(map[string]router.ModelMetadata)

	// 按 tier 分组收集模型
	tierModels := map[ModelTier][]config.Target{
		TierLite:      {},
		TierStandard:  {},
		TierLarge:     {},
		TierReasoning: {},
	}

	for _, providerName := range d.cfg.Discovery.Providers {
		provCfg, ok := d.cfg.Providers[providerName]
		if !ok {
			logger.Warnf("Discovery: provider %q not found in config, skipping", providerName)
			continue
		}

		apiKey := ""
		if len(provCfg.APIKeys) > 0 {
			apiKey = provCfg.APIKeys[0]
		}

		fetcher := NewFetcher(providerName)
		models, err := fetcher.FetchFreeModels(ctx, provCfg.BaseURL, apiKey)
		if err != nil {
			logger.Warnf("Discovery: failed to fetch models from %s: %v", providerName, err)
			continue
		}

		logger.Infof("Discovery: found %d free models from %s", len(models), providerName)

		for _, m := range models {
			// 注册单模型路由（不论是否聊天模型，都保留精确路由让用户可手动调用）
			routeName := m.ProviderName + "/" + m.ModelID
			allModels[routeName] = []config.Target{{
				Provider: m.ProviderName,
				Model:    m.ModelID,
				Weight:   1,
			}}

			// 优先用 OpenRouter 提供的 modality 过滤非聊天模型（如音频、OCR、图像生成），
			// 它们不进入 auto:* 与 free 聚合路由。其他 provider 没有 modality 时按聊天模型对待。
			if !IsTextGeneration(m.Modality) {
				logger.Debugf("Discovery: skipping non-chat model %s (modality=%q)", routeName, m.Modality)
				allMeta[routeName] = router.ModelMetadata{
					Provider: m.ProviderName,
					Source:   "discovery",
				}
				continue
			}

			// 多源解析参数量: model id → canonical_slug → hugging_face_id
			sizeB := ParseModelSize(m.ModelID, m.CanonicalSlug, m.HuggingFaceID)

			// 仍然解析不出且开启了 HF 兜底，调 HuggingFace API 查 safetensors.total
			if sizeB == 0 && d.hf != nil && m.HuggingFaceID != "" {
				if hfSize, _, err := d.hf.FetchModelInfo(ctx, m.HuggingFaceID); err == nil && hfSize > 0 {
					sizeB = hfSize
					logger.Debugf("Discovery: HF metadata for %s: %.1fB", m.HuggingFaceID, hfSize)
				} else if err != nil {
					logger.Debugf("Discovery: HF metadata fetch failed for %s: %v", m.HuggingFaceID, err)
				}
			}

			cap := ParseModelCapability(m.ModelID)
			tier := ClassifyTier(sizeB, cap)

			allMeta[routeName] = router.ModelMetadata{
				Tier:     string(tier),
				SizeB:    sizeB,
				Provider: m.ProviderName,
				Source:   "discovery",
			}

			// 用参数量作为权重（越大越优先被选中）
			weight := int(sizeB)
			if weight < 1 {
				weight = 1
			}

			tierModels[tier] = append(tierModels[tier], config.Target{
				Provider: m.ProviderName,
				Model:    m.ModelID,
				Weight:   weight,
			})
		}
	}

	// 自动注册 tier 路由: auto:lite, auto:standard, auto:large, auto:reasoning
	for tier, targets := range tierModels {
		if len(targets) == 0 {
			continue
		}
		routeName := "auto:" + string(tier)
		allModels[routeName] = targets
		allMeta[routeName] = router.ModelMetadata{
			Tier:   string(tier),
			Source: "alias",
		}
		logger.Infof("Discovery: auto:%s → %d models", tier, len(targets))
	}

	// 生成 "free" 聚合别名 — 从每个 tier 选权重最高的模型
	freeAlias := d.cfg.Discovery.FreeAlias
	if freeAlias == "" {
		freeAlias = "free"
	}
	if targets := buildFreeTargets(tierModels); len(targets) > 0 {
		allModels[freeAlias] = targets
		allMeta[freeAlias] = router.ModelMetadata{Source: "alias"}
		logger.Infof("Discovery: alias %q → %d targets", freeAlias, len(targets))
	}

	if len(allModels) > 0 {
		added, removed := d.router.UpdateDiscoveredModels(allModels)
		for name, meta := range allMeta {
			d.router.SetModelMetadata(name, meta)
		}
		logger.Infof("Discovery complete: %d routes, %d added, %d removed", len(allModels), added, removed)
	} else {
		logger.Infof("Discovery complete: no models discovered")
	}
}

// buildFreeTargets 从 standard 和 large tier 中选权重最高的模型组成 free 路由
func buildFreeTargets(tierModels map[ModelTier][]config.Target) []config.Target {
	var targets []config.Target

	// 优先从 large 和 standard 中各取 top 模型
	for _, tier := range []ModelTier{TierLarge, TierStandard, TierLite} {
		models := tierModels[tier]
		if len(models) == 0 {
			continue
		}
		// 按权重排序取最大的
		sorted := make([]config.Target, len(models))
		copy(sorted, models)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].Weight > sorted[j].Weight
		})
		best := sorted[0]
		best.Weight = 1 // free 路由中各 tier 权重均分
		targets = append(targets, best)
	}

	return targets
}

// TierRoutePrefix auto: 前缀
const TierRoutePrefix = "auto:"

// TierRouteName 返回 tier 对应的路由名
func TierRouteName(tier ModelTier) string {
	return TierRoutePrefix + string(tier)
}
