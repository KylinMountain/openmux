package cooldown

import (
	"sync"
	"time"
)

// Tracker 模型级别冷却追踪器
// 当某个 provider 的某个 model 返回 429 时，标记该模型为过热
// 同 provider 的其他模型不受影响
type Tracker struct {
	mu       sync.RWMutex
	cooldown map[string]time.Time // "provider/model" → cooldown until
	duration time.Duration
}

// NewTracker 创建冷却追踪器
func NewTracker(duration time.Duration) *Tracker {
	if duration <= 0 {
		duration = 30 * time.Second
	}
	return &Tracker{
		cooldown: make(map[string]time.Time),
		duration: duration,
	}
}

func modelKey(provider, model string) string {
	return provider + "/" + model
}

// MarkHot 标记模型过热
func (t *Tracker) MarkHot(provider, model string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cooldown[modelKey(provider, model)] = time.Now().Add(t.duration)
}

// IsHot 检查模型是否在冷却中
func (t *Tracker) IsHot(provider, model string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	until, ok := t.cooldown[modelKey(provider, model)]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		return false // 已过期，下次写入时清理
	}
	return true
}

// Cleanup 清理过期的冷却记录
func (t *Tracker) Cleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for k, until := range t.cooldown {
		if now.After(until) {
			delete(t.cooldown, k)
		}
	}
}

// HotCount 返回当前过热模型数
func (t *Tracker) HotCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	count := 0
	now := time.Now()
	for _, until := range t.cooldown {
		if now.Before(until) {
			count++
		}
	}
	return count
}
