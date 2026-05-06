package router

import (
	"fmt"
	"sync"
	"strings"

	"github.com/openmux/openmux/internal/config"
	"github.com/openmux/openmux/pkg/errors"
)

// TargetSelector 目标选择器接口
type TargetSelector interface {
	// Select 根据策略选择一个目标
	Select() (*config.Target, error)
	// GetAll 获取所有目标（用于重试）
	GetAll() []config.Target
}

// WeightedTargetSelector 加权目标选择器
type WeightedTargetSelector struct {
	mu            sync.Mutex
	targets       []config.Target
	current       int
	gcd           int
	maxWeight     int
	currentWeight int
}

// NewWeightedTargetSelector 创建加权目标选择器
func NewWeightedTargetSelector(targets []config.Target) *WeightedTargetSelector {
	if len(targets) == 0 {
		return &WeightedTargetSelector{}
	}

	weights := make([]int, 0, len(targets))
	for _, target := range targets {
		weight := target.Weight
		if weight <= 0 {
			weight = 1
		}
		weights = append(weights, weight)
	}

	gcd := gcdSlice(weights)
	maxWeight := maxSlice(weights)

	return &WeightedTargetSelector{
		targets:       targets,
		current:       -1,
		gcd:           gcd,
		maxWeight:     maxWeight,
		currentWeight: 0,
	}
}

// Select 选择一个目标
func (w *WeightedTargetSelector) Select() (*config.Target, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.targets) == 0 {
		return nil, errors.New(errors.ErrCodeNoAvailableBackend, "no targets available")
	}

	// 加权轮询算法
	for i := 0; i < len(w.targets)*w.maxWeight; i++ {
		w.current = (w.current + 1) % len(w.targets)

		if w.current == 0 {
			w.currentWeight -= w.gcd
			if w.currentWeight <= 0 {
				w.currentWeight = w.maxWeight
			}
		}

		target := &w.targets[w.current]
		if target.Weight >= w.currentWeight {
			return target, nil
		}
	}

	// 如果所有目标权重都不满足，返回第一个
	return &w.targets[0], nil
}

// GetAll 获取所有目标
func (w *WeightedTargetSelector) GetAll() []config.Target {
	w.mu.Lock()
	defer w.mu.Unlock()
	result := make([]config.Target, len(w.targets))
	copy(result, w.targets)
	return result
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

// Router 模型路由器
type Router struct {
	mu                sync.RWMutex
	routes            map[string]TargetSelector
	staticRoutes      map[string]bool // 静态配置的路由（不会被动态发现覆盖）
	passthrough       bool
	allowedProviders  map[string]bool
	providers         map[string]bool
}

// NewRouter 创建路由器
func NewRouter(cfg *config.Config) *Router {
	providers := make(map[string]bool)
	for name := range cfg.Providers {
		providers[name] = true
	}

	// 为每个路由创建选择器
	routes := make(map[string]TargetSelector)
	for name, route := range cfg.ModelRoutes {
		var selector TargetSelector
		switch route.Strategy {
		case "weighted_round_robin", "":
			selector = NewWeightedTargetSelector(route.Targets)
		default:
			// 默认使用加权轮询
			selector = NewWeightedTargetSelector(route.Targets)
		}
		routes[name] = selector
	}

	staticRoutes := make(map[string]bool, len(routes))
	for name := range routes {
		staticRoutes[name] = true
	}

	return &Router{
		routes:           routes,
		staticRoutes:     staticRoutes,
		passthrough:      true,
		allowedProviders: make(map[string]bool),
		providers:        providers,
	}
}

// Route 路由模型请求，返回目标选择器
func (r *Router) Route(modelName string) (TargetSelector, error) {
	r.mu.RLock()
	selector, ok := r.routes[modelName]
	r.mu.RUnlock()

	// 1. 优先匹配自定义别名
	if ok {
		return selector, nil
	}

	// 2. 尝试解析 provider/model 格式
	if r.passthrough {
		provider, model, ok := parseProviderModel(modelName)
		if ok {
			// 检查 provider 是否存在
			if !r.providers[provider] {
				return nil, errors.New(errors.ErrCodeModelNotFound,
					fmt.Sprintf("provider not found: %s", provider))
			}

			// 返回单个目标的选择器
			return NewWeightedTargetSelector([]config.Target{{
				Provider: provider,
				Model:    model,
				Weight:   1,
			}}), nil
		}
	}

	return nil, errors.New(errors.ErrCodeModelNotFound,
		fmt.Sprintf("model not found: %s", modelName))
}

// ListModels 列出所有可用模型
func (r *Router) ListModels() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	models := make([]string, 0, len(r.routes))
	for name := range r.routes {
		models = append(models, name)
	}
	return models
}

// RouteTargets 路由模型请求，返回所有目标（用于兼容旧代码）
func (r *Router) RouteTargets(modelName string) ([]config.Target, error) {
	selector, err := r.Route(modelName)
	if err != nil {
		return nil, err
	}
	return selector.GetAll(), nil
}

// UpdateDiscoveredModels 动态更新发现的模型路由（不会覆盖静态配置）
func (r *Router) UpdateDiscoveredModels(models map[string][]config.Target) (added, removed int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 记录当前的动态路由
	currentDynamic := make(map[string]bool)
	for name := range r.routes {
		if !r.staticRoutes[name] {
			currentDynamic[name] = true
		}
	}

	// 添加/更新动态路由
	for name, targets := range models {
		if r.staticRoutes[name] {
			continue // 不覆盖静态路由
		}
		if _, exists := r.routes[name]; !exists {
			added++
		}
		r.routes[name] = NewWeightedTargetSelector(targets)
		delete(currentDynamic, name)
	}

	// 移除不再存在的动态路由
	for name := range currentDynamic {
		delete(r.routes, name)
		removed++
	}

	return added, removed
}

// parseProviderModel 解析 provider/model 或 provider:model 格式
func parseProviderModel(modelName string) (provider, model string, ok bool) {
	// 尝试解析 provider:model
	parts := strings.SplitN(modelName, ":", 2)
	if len(parts) == 2 {
		return parts[0], parts[1], true
	}
	
	// 尝试解析 provider/model
	parts = strings.SplitN(modelName, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1], true
	}
	return "", "", false
}
