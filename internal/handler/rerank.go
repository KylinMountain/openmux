package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/openmux/openmux/internal/balancer"
	"github.com/openmux/openmux/internal/config"
	"github.com/openmux/openmux/internal/provider"
	"github.com/openmux/openmux/internal/router"
	"github.com/openmux/openmux/pkg/errors"
	"github.com/openmux/openmux/pkg/logger"
	pkgopenai "github.com/openmux/openmux/pkg/openai"
	"github.com/openmux/openmux/pkg/tokenizer"
)

// RerankHandler Rerank 处理器
type RerankHandler struct {
	router         *router.Router
	providerPool   *provider.Pool
	balancerPool   *balancer.BalancerPool
}

// NewRerankHandler 创建 Rerank 处理器
func NewRerankHandler(
	router *router.Router,
	providerPool *provider.Pool,
	balancerPool *balancer.BalancerPool,
) *RerankHandler {
	return &RerankHandler{
		router:       router,
		providerPool: providerPool,
		balancerPool: balancerPool,
	}
}

// Handle 处理 Rerank 请求
func (h *RerankHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	var req pkgopenai.RerankRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	targetSelector, err := h.router.Route(req.Model)
	if err != nil {
		if e, ok := err.(*errors.Error); ok {
			writeError(w, http.StatusNotFound, string(e.Code), e.Message)
		} else {
			writeError(w, http.StatusNotFound, "model_not_found", err.Error())
		}
		return
	}

	resp, err := h.handleWithRetry(r.Context(), &req, targetSelector)
	if err != nil {
		logger.Errorf("Rerank failed: %v", err)
		if e, ok := err.(*errors.Error); ok {
			msg := e.Message
			if e.Err != nil {
				msg = fmt.Sprintf("%s: %v", e.Message, e.Err)
			}
			writeError(w, http.StatusInternalServerError, string(e.Code), msg)
		} else {
			writeError(w, http.StatusInternalServerError, "provider_error", err.Error())
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *RerankHandler) handleWithRetry(
	ctx context.Context,
	req *pkgopenai.RerankRequest,
	targetSelector router.TargetSelector,
) (*pkgopenai.RerankResponse, error) {
	var lastErr error

	target, err := targetSelector.Select()
	if err == nil {
		resp, tryErr := h.tryTarget(ctx, req, target)
		if tryErr == nil {
			return resp, nil
		}
		logger.Warnf("Selected target %s/%s failed: %v", target.Provider, target.Model, tryErr)
		lastErr = tryErr
	}

	allTargets := targetSelector.GetAll()
	for _, target := range allTargets {
		resp, err := h.tryTarget(ctx, req, &target)
		if err == nil {
			return resp, nil
		}
		logger.Warnf("Target %s/%s failed: %v", target.Provider, target.Model, err)

		if errors.IsRateLimitError(err) {
			lastErr = err
			continue
		}
		if errors.IsRetryable(err) {
			lastErr = err
			continue
		}
		return nil, err
	}

	return nil, errors.Wrap(errors.ErrCodeProviderError, "all targets failed", lastErr)
}

func (h *RerankHandler) tryTarget(
	ctx context.Context,
	req *pkgopenai.RerankRequest,
	target *config.Target,
) (*pkgopenai.RerankResponse, error) {
	estimatedTokens := h.estimateTokens(req)
	backend, err := h.selectBackend(target.Provider, estimatedTokens)
	if err != nil {
		return nil, err
	}
	
	actualUsage := 0
	defer func() {
		h.releaseBackend(target.Provider, backend, actualUsage, estimatedTokens)
	}()

	prov, err := h.providerPool.Get(target.Provider)
	if err != nil {
		return nil, err
	}

	resp, err := prov.Rerank(ctx, req, target.Model, backend.APIKey)

	if resp != nil {
		actualUsage = resp.Usage.TotalTokens
	}

	if err != nil {
		if errors.IsRateLimitError(err) {
			h.markBackendUnhealthy(target.Provider, backend)
		}
		return nil, err
	}

	return resp, nil
}

func (h *RerankHandler) selectBackend(providerName string, estimatedTokens int) (*balancer.Backend, error) {
	bal, err := h.balancerPool.Get(providerName)
	if err != nil {
		return nil, err
	}
	return bal.Select(estimatedTokens)
}

func (h *RerankHandler) releaseBackend(providerName string, backend *balancer.Backend, actualUsage, estimatedTokens int) {
	if bal, err := h.balancerPool.Get(providerName); err == nil {
		bal.Release(backend, actualUsage, estimatedTokens)
	}
}

func (h *RerankHandler) markBackendUnhealthy(providerName string, backend *balancer.Backend) {
	if bal, err := h.balancerPool.Get(providerName); err == nil {
		bal.MarkUnhealthy(backend)
	}
}

func (h *RerankHandler) estimateTokens(req *pkgopenai.RerankRequest) int {
	tkm, err := tokenizer.GetEncoding(req.Model)
	if err != nil {
		// Fallback to simple estimation
		lenDocs := 0
		for _, doc := range req.Documents {
			lenDocs += len(doc)
		}
		return (len(req.Query) + lenDocs) / 4
	}

	count := len(tkm.Encode(req.Query, nil, nil))
	for _, doc := range req.Documents {
		count += len(tkm.Encode(doc, nil, nil))
	}
	return count
}
