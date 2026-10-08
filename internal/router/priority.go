package router

import (
	"sync"

	"github.com/openmux/openmux/internal/config"
	"github.com/openmux/openmux/pkg/errors"
)

// PriorityTargetSelector 按配置顺序选择目标：Select 总是返回第一个，
// 失败后由调用方按 GetAll 的顺序依次回退（跳过冷却中的目标由 handler 负责）。
type PriorityTargetSelector struct {
	mu      sync.Mutex
	targets []config.Target
}

// NewPriorityTargetSelector 创建按顺序回退的目标选择器
func NewPriorityTargetSelector(targets []config.Target) *PriorityTargetSelector {
	cp := make([]config.Target, len(targets))
	copy(cp, targets)
	return &PriorityTargetSelector{targets: cp}
}

// Select 返回配置里的第一个目标
func (p *PriorityTargetSelector) Select() (*config.Target, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.targets) == 0 {
		return nil, errors.New(errors.ErrCodeNoAvailableBackend, "no targets available")
	}
	first := p.targets[0]
	return &first, nil
}

// GetAll 按配置顺序返回所有目标
func (p *PriorityTargetSelector) GetAll() []config.Target {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]config.Target, len(p.targets))
	copy(out, p.targets)
	return out
}
