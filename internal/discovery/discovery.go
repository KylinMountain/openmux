package discovery

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/openmux/openmux/internal/config"
	"github.com/openmux/openmux/internal/router"
	"github.com/openmux/openmux/pkg/logger"
)

// ModelDiscovery 模型自动发现服务
// 定时调用各 provider 的 /v1/models 接口，筛选免费模型并动态更新路由表
type ModelDiscovery struct {
	cfg    *config.Config
	router *router.Router
	cancel context.CancelFunc
}

// NewModelDiscovery 创建模型发现服务
func NewModelDiscovery(cfg *config.Config, r *router.Router) *ModelDiscovery {
	return &ModelDiscovery{
		cfg:    cfg,
		router: r,
	}
}

// Start 启动发现服务（阻塞，应在 goroutine 中调用）
func (d *ModelDiscovery) Start(ctx context.Context) {
	ctx, d.cancel = context.WithCancel(ctx)

	// 启动时立即执行一次
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

// Stop 停止发现服务
func (d *ModelDiscovery) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
}

// discoverOnce 执行一次发现
func (d *ModelDiscovery) discoverOnce(ctx context.Context) {
	logger.Infof("Starting model discovery...")

	allModels := make(map[string][]config.Target)
	// 按 provider 分组收集免费模型，用于生成聚合路由
	providerModels := make(map[string][]DiscoveredModel)

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
		providerModels[providerName] = models

		for _, m := range models {
			// 以 provider/model 格式注册路由
			routeName := m.ProviderName + "/" + m.ModelID
			allModels[routeName] = []config.Target{{
				Provider: m.ProviderName,
				Model:    m.ModelID,
				Weight:   1,
			}}
		}
	}

	// 生成聚合 "free" 别名路由
	// 每个 provider 选第一个模型作为 target，权重均分，实现跨平台 fallback
	freeAlias := d.cfg.Discovery.FreeAlias
	if freeAlias == "" {
		freeAlias = "free"
	}
	if targets := buildFreeAliasTargets(providerModels); len(targets) > 0 {
		allModels[freeAlias] = targets
		logger.Infof("Discovery: registered alias %q with %d targets", freeAlias, len(targets))
	}

	if len(allModels) > 0 {
		added, removed := d.router.UpdateDiscoveredModels(allModels)
		logger.Infof("Discovery complete: %d routes total, %d added, %d removed", len(allModels), added, removed)
	} else {
		logger.Infof("Discovery complete: no models discovered")
	}
}

// buildFreeAliasTargets 从每个 provider 的免费模型中选一个代表，组成聚合路由
// 排序策略：按模型参数量/知名度粗排，大模型优先
func buildFreeAliasTargets(providerModels map[string][]DiscoveredModel) []config.Target {
	var targets []config.Target

	// 按 provider 名称排序，保证稳定性
	providerNames := make([]string, 0, len(providerModels))
	for name := range providerModels {
		providerNames = append(providerNames, name)
	}
	sort.Strings(providerNames)

	for _, providerName := range providerNames {
		models := providerModels[providerName]
		if len(models) == 0 {
			continue
		}
		// 选最佳免费模型作为该 provider 的代表
		best := selectBestModel(models)
		targets = append(targets, config.Target{
			Provider: best.ProviderName,
			Model:    best.ModelID,
			Weight:   1,
		})
	}
	return targets
}

// selectBestModel 从模型列表中选择"最佳"免费模型
// 启发式：优先选参数量大的知名模型
func selectBestModel(models []DiscoveredModel) DiscoveredModel {
	if len(models) == 1 {
		return models[0]
	}

	// 优先关键词列表（从高到低）
	priorities := []string{
		"qwen3", "qwen-2.5-72b", "deepseek-v3", "deepseek-r1",
		"llama-3.3-70b", "gemini", "glm-4",
		"qwen2.5", "llama", "mistral",
	}

	id := func(m DiscoveredModel) string {
		return strings.ToLower(m.ModelID)
	}

	for _, keyword := range priorities {
		for _, m := range models {
			if strings.Contains(id(m), keyword) {
				return m
			}
		}
	}

	// 没匹配到，返回第一个
	return models[0]
}
