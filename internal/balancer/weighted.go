package balancer

import (
	"fmt"
	"sync"
	"time"

	"github.com/openmux/openmux/internal/config"
	"github.com/openmux/openmux/internal/ratelimit"
	"github.com/openmux/openmux/pkg/errors"
)

// WeightedRoundRobin 加权轮询负载均衡器
type WeightedRoundRobin struct {
	mu            sync.Mutex
	backends      []*Backend
	current       int
	gcd           int
	maxWeight     int
	currentWeight int
}

// NewWeightedRoundRobin 创建加权轮询负载均衡器
func NewWeightedRoundRobin(provider string, apiKeys []string, rateLimit config.RateLimit, healthCheck config.HealthCheckConfig) *WeightedRoundRobin {
	backends := make([]*Backend, 0, len(apiKeys))
	weights := make([]int, 0, len(apiKeys))
	
	for _, key := range apiKeys {
		if key == "" {
			continue
		}
		// 默认权重为 1
		weight := 1
		backends = append(backends, &Backend{
			Provider:  provider,
			APIKey:    key,
			RateLimit: rateLimit,
			Limiter:   ratelimit.NewMultiLimiter(rateLimit.RPM, rateLimit.TPM, rateLimit.RPD),
			Weight:    weight,
			Healthy:   true,
		})
		weights = append(weights, weight)
	}
	
	gcd := gcdSlice(weights)
	maxWeight := maxSlice(weights)
	
	w := &WeightedRoundRobin{
		backends:      backends,
		current:       -1,
		gcd:           gcd,
		maxWeight:     maxWeight,
		currentWeight: 0,
	}

	// 总是启动健康检查（自动恢复），如果没有配置则使用默认值
	w.startHealthCheck(healthCheck)

	return w
}

// Select 选择一个后端
func (w *WeightedRoundRobin) Select(estimatedTokens int) (*Backend, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	if len(w.backends) == 0 {
		return nil, errors.New(errors.ErrCodeNoAvailableBackend, "no backends available")
	}
	
	// 尝试选择后端
	for i := 0; i < len(w.backends)*w.maxWeight; i++ {
		w.current = (w.current + 1) % len(w.backends)
		
		if w.current == 0 {
			w.currentWeight -= w.gcd
			if w.currentWeight <= 0 {
				w.currentWeight = w.maxWeight
			}
		}
		
		backend := w.backends[w.current]
		if backend.Weight >= w.currentWeight && backend.AcquireConn(estimatedTokens) {
			return backend, nil
		}
	}
	
	return nil, errors.New(errors.ErrCodeNoAvailableBackend, "all backends are busy or unhealthy")
}

// Release 释放后端连接
func (w *WeightedRoundRobin) Release(backend *Backend, usedTokens, estimatedTokens int) {
	backend.ReleaseConn(usedTokens, estimatedTokens)
}

// UnhealthyThreshold 连续失败多少次才标记 unhealthy
const UnhealthyThreshold = 3

// MarkUnhealthy 标记后端不健康（需连续失败达到阈值）
func (w *WeightedRoundRobin) MarkUnhealthy(backend *Backend) {
	failCount := backend.IncrFailCount()
	backend.LastFailure = time.Now()
	if failCount >= int32(UnhealthyThreshold) {
		w.mu.Lock()
		backend.Healthy = false
		w.mu.Unlock()
	}
}

// MarkHealthy 标记后端健康
func (w *WeightedRoundRobin) MarkHealthy(backend *Backend) {
	w.mu.Lock()
	defer w.mu.Unlock()
	backend.Healthy = true
	backend.ResetFailCount()
}

// GetBackends 获取所有后端
func (w *WeightedRoundRobin) GetBackends() []*Backend {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.backends
}

// gcd 计算最大公约数
func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// gcdSlice 计算切片的最大公约数
func gcdSlice(nums []int) int {
	if len(nums) == 0 {
		return 1
	}
	result := nums[0]
	for i := 1; i < len(nums); i++ {
		result = gcd(result, nums[i])
	}
	return result
}

// maxSlice 获取切片最大值
func maxSlice(nums []int) int {
	if len(nums) == 0 {
		return 0
	}
	max := nums[0]
	for _, n := range nums[1:] {
		if n > max {
			max = n
		}
	}
	return max
}

// BalancerPool 负载均衡器池
type BalancerPool struct {
	mu        sync.RWMutex
	balancers map[string]Balancer
}

// NewBalancerPool 创建负载均衡器池
func NewBalancerPool() *BalancerPool {
	return &BalancerPool{
		balancers: make(map[string]Balancer),
	}
}

// Register 注册负载均衡器
func (p *BalancerPool) Register(provider string, balancer Balancer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.balancers[provider] = balancer
}

// Get 获取负载均衡器
func (p *BalancerPool) Get(provider string) (Balancer, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	balancer, ok := p.balancers[provider]
	if !ok {
		return nil, fmt.Errorf("balancer not found for provider: %s", provider)
	}
	return balancer, nil
}

// startHealthCheck 启动健康检查
func (w *WeightedRoundRobin) startHealthCheck(cfg config.HealthCheckConfig) {
	interval := cfg.Interval
	if interval <= 0 {
		interval = 60 * time.Second
	}

	ticker := time.NewTicker(interval)
	go func() {
		for range ticker.C {
			w.mu.Lock()
			for _, backend := range w.backends {
				if !backend.Healthy {
					// 简单的基于时间的恢复策略
					// 如果后端不健康的时间超过了检测间隔，尝试恢复
					if time.Since(backend.LastFailure) >= interval {
						backend.Healthy = true
						backend.ResetFailCount()
					}
				}
			}
			w.mu.Unlock()
		}
	}()
}

// InitFromConfig 从配置初始化负载均衡器池
func InitFromConfig(cfg *config.Config) *BalancerPool {
	pool := NewBalancerPool()

	for name, providerCfg := range cfg.Providers {
		var balancer Balancer
		var strategy string
		var healthCheck config.HealthCheckConfig

		// 检查 per-provider 的负载均衡配置
		if providerCfg.LoadBalancer != nil {
			strategy = providerCfg.LoadBalancer.Strategy
			healthCheck = providerCfg.LoadBalancer.HealthCheck
		}

		// 如果未配置或为空，则使用默认策略
		if strategy == "" {
			strategy = "weighted_round_robin" // 默认策略
		}

		switch strategy {
		case "weighted_round_robin":
			balancer = NewWeightedRoundRobin(name, providerCfg.APIKeys, providerCfg.RateLimit, healthCheck)
		default:
			// 默认回退到 weighted_round_robin
			balancer = NewWeightedRoundRobin(name, providerCfg.APIKeys, providerCfg.RateLimit, healthCheck)
		}

		pool.Register(name, balancer)
	}

	return pool
}
