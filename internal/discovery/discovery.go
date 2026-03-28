package discovery

import (
	"context"
	"time"

	"github.com/openmux/openmux/internal/config"
	"github.com/openmux/openmux/internal/router"
	"github.com/openmux/openmux/pkg/logger"
)

// ModelDiscovery 模型自动发现服务
// 定时调用各 provider 的 /v1/models 接口，筛选免费模型并动态更新路由表
type ModelDiscovery struct {
	cfg      *config.Config
	router   *router.Router
	cancel   context.CancelFunc
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
			// 以 provider/model 格式注册路由
			routeName := m.ProviderName + "/" + m.ModelID
			allModels[routeName] = []config.Target{{
				Provider: m.ProviderName,
				Model:    m.ModelID,
				Weight:   1,
			}}
		}
	}

	if len(allModels) > 0 {
		added, removed := d.router.UpdateDiscoveredModels(allModels)
		logger.Infof("Discovery complete: %d models total, %d added, %d removed", len(allModels), added, removed)
	} else {
		logger.Infof("Discovery complete: no models discovered")
	}
}
