package auth

import (
	"sync"

	"github.com/openmux/openmux/internal/config"
	"github.com/openmux/openmux/internal/ratelimit"
)

// ClientInfo 客户端信息
type ClientInfo struct {
	Key     string
	Name    string
	Limiter *ratelimit.MultiLimiter
}

// Manager 认证管理器
type Manager struct {
	mu      sync.RWMutex
	clients map[string]*ClientInfo
	enabled bool
}

// NewManager 创建认证管理器
func NewManager(cfg *config.AuthConfig) *Manager {
	clients := make(map[string]*ClientInfo)
	
	for _, apiKey := range cfg.APIKeys {
		clients[apiKey.Key] = &ClientInfo{
			Key:     apiKey.Key,
			Name:    apiKey.Name,
			Limiter: ratelimit.NewMultiLimiter(apiKey.RateLimit.RPM, apiKey.RateLimit.TPM, apiKey.RateLimit.RPD),
		}
	}
	
	return &Manager{
		clients: clients,
		enabled: cfg.Enabled,
	}
}

// Verify 验证 API Key
func (m *Manager) Verify(apiKey string) (*ClientInfo, bool) {
	if !m.enabled {
		return &ClientInfo{Key: "anonymous", Name: "anonymous"}, true
	}
	
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	client, ok := m.clients[apiKey]
	return client, ok
}

// CheckRateLimit 检查限流
func (m *Manager) CheckRateLimit(client *ClientInfo) bool {
	if !m.enabled || client.Limiter == nil {
		return true
	}
	return client.Limiter.Allow()
}
