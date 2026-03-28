package config

import "time"

// Config 主配置结构
type Config struct {
	Server       ServerConfig              `yaml:"server"`
	Auth         AuthConfig                `yaml:"auth"`
	Providers    map[string]ProviderConfig `yaml:"providers"`
	ModelRoutes  map[string]ModelRouteConfig `yaml:"model_routes"`
	Passthrough  PassthroughConfig         `yaml:"passthrough"`
	Monitoring   MonitoringConfig          `yaml:"monitoring"`
	Cache        CacheConfig               `yaml:"cache"`
	Discovery    DiscoveryConfig           `yaml:"discovery"`
}

// DiscoveryConfig 模型自动发现配置
type DiscoveryConfig struct {
	Enabled   bool          `yaml:"enabled"`
	Interval  time.Duration `yaml:"interval"`   // 刷新间隔，默认 24h
	Providers []string      `yaml:"providers"`  // 需要发现的 provider 列表
}

// ServerConfig 服务器配置
type ServerConfig struct {
	Port         int           `yaml:"port"`
	Host         string        `yaml:"host"`
	Timeout      time.Duration `yaml:"timeout"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`
}

// AuthConfig 认证配置
type AuthConfig struct {
	Enabled bool               `yaml:"enabled"`
	APIKeys []ClientAPIKeyInfo `yaml:"api_keys"`
}

// ClientAPIKeyInfo 客户端 API Key 信息
type ClientAPIKeyInfo struct {
	Key       string     `yaml:"key"`
	Name      string     `yaml:"name"`
	RateLimit RateLimit  `yaml:"rate_limit"`
}

// RateLimit 限流配置
type RateLimit struct {
	RPM        int `yaml:"rpm"`        // 每分钟请求数
	TPM        int `yaml:"tpm"`        // 每分钟 token 数
	RPD        int `yaml:"rpd"`        // 每天请求数
	Concurrent int `yaml:"concurrent"` // 最大并发数
}

// ProviderConfig Provider 配置
type ProviderConfig struct {
	BaseURL      string              `yaml:"base_url"`
	Type         string              `yaml:"type"`
	Timeout      time.Duration       `yaml:"timeout"`
	Retry        RetryConfig         `yaml:"retry"`
	APIKeys      []string            `yaml:"api_keys"`
	RateLimit    RateLimit           `yaml:"rate_limit"`
	LoadBalancer *LoadBalancerConfig `yaml:"load_balancer,omitempty"`
}

// RetryConfig 重试配置
type RetryConfig struct {
	MaxAttempts int    `yaml:"max_attempts"`
	Backoff     string `yaml:"backoff"`
}

// ModelRouteConfig 模型路由配置
type ModelRouteConfig struct {
	Description string   `yaml:"description"`
	Targets     []Target `yaml:"targets"`
	Strategy    string   `yaml:"strategy"`
}

// Target 目标配置
type Target struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
	Weight   int    `yaml:"weight"`
}

// PassthroughConfig 直通模式配置
type PassthroughConfig struct {
	Enabled          bool     `yaml:"enabled"`
	AllowedProviders []string `yaml:"allowed_providers"`
}

// LoadBalancerConfig 负载均衡配置
type LoadBalancerConfig struct {
	Strategy    string            `yaml:"strategy"`
	HealthCheck HealthCheckConfig `yaml:"health_check"`
}

// HealthCheckConfig 健康检查配置
type HealthCheckConfig struct {
	Enabled            bool          `yaml:"enabled"`
	Interval           time.Duration `yaml:"interval"`
	Timeout            time.Duration `yaml:"timeout"`
	UnhealthyThreshold int           `yaml:"unhealthy_threshold"`
	HealthyThreshold   int           `yaml:"healthy_threshold"`
}

// MonitoringConfig 监控配置
type MonitoringConfig struct {
	Enabled     bool   `yaml:"enabled"`
	MetricsPath string `yaml:"metrics_path"`
	LogLevel    string `yaml:"log_level"`
}

// CacheConfig 缓存配置
type CacheConfig struct {
	Enabled bool          `yaml:"enabled"`
	TTL     time.Duration `yaml:"ttl"`
	MaxSize int           `yaml:"max_size"`
}
