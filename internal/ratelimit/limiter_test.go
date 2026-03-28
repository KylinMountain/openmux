package ratelimit

import (
	"testing"
	"time"
)

func TestTokenBucket(t *testing.T) {
	// 创建一个每分钟 60 个请求的限流器（每秒 1 个）
	limiter := NewTokenBucket(60)

	// 第一个请求应该通过
	if !limiter.Allow() {
		t.Error("First request should be allowed")
	}

	// 连续请求应该被限制
	allowed := 0
	for i := 0; i < 100; i++ {
		if limiter.Allow() {
			allowed++
		}
	}

	// 应该只有初始的 token 数量被允许
	if allowed > 60 {
		t.Errorf("Too many requests allowed: %d", allowed)
	}

	// 等待一段时间后应该可以再次请求
	time.Sleep(1100 * time.Millisecond)
	if !limiter.Allow() {
		t.Error("Request should be allowed after waiting")
	}
}

func TestMultiLimiter(t *testing.T) {
	limiter := NewMultiLimiter(60, 1000, 0)

	// 第一个请求应该通过 (Reserve 0 tokens also checks RPM)
	if !limiter.Reserve(0) {
		t.Error("First request should be allowed")
	}

	// 测试 token 限流
	if !limiter.Reserve(100) {
		t.Error("Should allow 100 tokens")
	}

	// 超过限制应该被拒绝
	if limiter.Reserve(10000) {
		t.Error("Should not allow 10000 tokens")
	}
}

func TestMultiLimiterRPD(t *testing.T) {
	// RPD=5, RPM 不限
	limiter := NewMultiLimiter(0, 0, 5)

	for i := 0; i < 5; i++ {
		if !limiter.Reserve(0) {
			t.Errorf("Request %d should be allowed", i+1)
		}
	}

	// 第 6 个应该被拒绝
	if limiter.Reserve(0) {
		t.Error("6th request should be denied by RPD limit")
	}
}