package ratelimit

import (
	"sync"
	"time"
)

// Limiter 限流器接口
type Limiter interface {
	Allow() bool
	AllowN(n int) bool
}

// TokenBucket Token Bucket 限流器
type TokenBucket struct {
	mu           sync.Mutex
	capacity     int
	tokens       float64
	refillRate   float64
	lastRefill   time.Time
}

// NewTokenBucket 创建 Token Bucket 限流器（按分钟填充）
func NewTokenBucket(perMinute int) *TokenBucket {
	capacity := perMinute
	if capacity <= 0 {
		capacity = 1000000 // 无限制
	}

	return &TokenBucket{
		capacity:   capacity,
		tokens:     float64(capacity),
		refillRate: float64(capacity) / 60.0, // 每秒补充的 token 数
		lastRefill: time.Now(),
	}
}

// NewDailyTokenBucket 创建按天限流的 Token Bucket
func NewDailyTokenBucket(perDay int) *TokenBucket {
	capacity := perDay
	if capacity <= 0 {
		capacity = 1000000 // 无限制
	}

	return &TokenBucket{
		capacity:   capacity,
		tokens:     float64(capacity),
		refillRate: float64(capacity) / 86400.0, // 每秒补充 = 总量 / 86400秒
		lastRefill: time.Now(),
	}
}

// Allow 检查是否允许请求
func (tb *TokenBucket) Allow() bool {
	return tb.AllowN(1)
}

// AllowN 检查是否允许 n 个请求
func (tb *TokenBucket) AllowN(n int) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	
	// 补充 token
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > float64(tb.capacity) {
		tb.tokens = float64(tb.capacity)
	}
	tb.lastRefill = now
	
	// 检查是否有足够的 token
	if tb.tokens >= float64(n) {
		tb.tokens -= float64(n)
		return true
	}
	
	return false
}

// Return 归还 token
func (tb *TokenBucket) Return(n float64) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	
	tb.tokens += n
	if tb.tokens > float64(tb.capacity) {
		tb.tokens = float64(tb.capacity)
	}
}

// Consume 强制消耗 token (允许透支)
func (tb *TokenBucket) Consume(n float64) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	
	// 先更新
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill).Seconds()
	tb.tokens += elapsed * tb.refillRate
	if tb.tokens > float64(tb.capacity) {
		tb.tokens = float64(tb.capacity)
	}
	tb.lastRefill = now

	tb.tokens -= n
}

// MultiLimiter 多维度限流器
type MultiLimiter struct {
	rpmLimiter *TokenBucket
	tpmLimiter *TokenBucket
	rpdLimiter *TokenBucket // 每日请求数限流
}

// NewMultiLimiter 创建多维度限流器
func NewMultiLimiter(rpm, tpm, rpd int) *MultiLimiter {
	return &MultiLimiter{
		rpmLimiter: NewTokenBucket(rpm),
		tpmLimiter: NewTokenBucket(tpm),
		rpdLimiter: NewDailyTokenBucket(rpd),
	}
}

// Allow 检查是否允许请求 (RPM check)
func (ml *MultiLimiter) Allow() bool {
	return ml.rpmLimiter.Allow()
}

// Reserve 预留 Token
func (ml *MultiLimiter) Reserve(estimatedTokens int) bool {
	// 检查 RPD
	if ml.rpdLimiter.capacity < 1000000 {
		if !ml.rpdLimiter.Allow() {
			return false
		}
	}

	if !ml.rpmLimiter.Allow() {
		// RPM 不足，归还 RPD
		if ml.rpdLimiter.capacity < 1000000 {
			ml.rpdLimiter.Return(1)
		}
		return false
	}

	// 如果配置了 TPM 限制 (tpm > 0)，则检查 TPM
	if ml.tpmLimiter.capacity < 1000000 {
		if !ml.tpmLimiter.AllowN(estimatedTokens) {
			// TPM 不足，归还 RPM 和 RPD
			ml.rpmLimiter.Return(1)
			if ml.rpdLimiter.capacity < 1000000 {
				ml.rpdLimiter.Return(1)
			}
			return false
		}
	}
	return true
}

// Update 更新实际消耗
func (ml *MultiLimiter) Update(usedTokens, estimatedTokens int) {
	diff := float64(estimatedTokens - usedTokens)
	if diff > 0 {
		// 预估多了，归还
		ml.tpmLimiter.Return(diff)
	} else if diff < 0 {
		// 预估少了，补扣 (允许透支)
		ml.tpmLimiter.Consume(-diff)
	}
}

