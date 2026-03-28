package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/openai/openai-go"
	"github.com/openmux/openmux/internal/autoroute"
	"github.com/openmux/openmux/internal/balancer"
	"github.com/openmux/openmux/internal/config"
	"github.com/openmux/openmux/internal/provider"
	"github.com/openmux/openmux/internal/router"
	"github.com/openmux/openmux/pkg/errors"
	"github.com/openmux/openmux/pkg/logger"
	pkgopenai "github.com/openmux/openmux/pkg/openai"
	"github.com/openmux/openmux/pkg/tokenizer"
)

// ChatHandler 聊天补全处理器
type ChatHandler struct {
	router         *router.Router
	providerPool   *provider.Pool
	balancerPool   *balancer.BalancerPool
	autoRouter     *autoroute.AutoRouter
}

// NewChatHandler 创建聊天处理器
func NewChatHandler(
	router *router.Router,
	providerPool *provider.Pool,
	balancerPool *balancer.BalancerPool,
	autoRouter *autoroute.AutoRouter,
) *ChatHandler {
	return &ChatHandler{
		router:       router,
		providerPool: providerPool,
		balancerPool: balancerPool,
		autoRouter:   autoRouter,
	}
}

// Handle 处理聊天补全请求
func (h *ChatHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	// 解析请求
	var req pkgopenai.ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid request body")
		return
	}

	// DEBUG: Log tools and tool_choice
	if len(req.Tools) > 0 || req.ToolChoice != nil {
		logger.Debugf("Request contains Tools: %d, ToolChoice: %v", len(req.Tools), req.ToolChoice)
		// Option to log full tools content if needed, but count is good start
	}

	// 智能路由：如果 model 是 auto 别名，根据请求复杂度选择实际路由
	modelName := req.Model
	if h.autoRouter != nil && modelName == h.autoRouter.Alias() {
		modelName = h.autoRouter.Resolve(r.Context(), &req)
		if modelName == "" {
			writeError(w, http.StatusBadRequest, "auto_route_error", "No model tier configured for auto routing")
			return
		}
	}

	// 路由模型
	targetSelector, err := h.router.Route(modelName)
	if err != nil {
		if e, ok := err.(*errors.Error); ok {
			writeError(w, http.StatusNotFound, string(e.Code), e.Message)
		} else {
			writeError(w, http.StatusNotFound, "model_not_found", err.Error())
		}
		return
	}

	// 处理请求
	if req.Stream {
		h.handleStream(w, r, &req, targetSelector)
	} else {
		h.handleNonStream(w, r, &req, targetSelector)
	}
}

// handleNonStream 处理非流式请求
func (h *ChatHandler) handleNonStream(
	w http.ResponseWriter,
	r *http.Request,
	req *pkgopenai.ChatCompletionRequest,
	targetSelector router.TargetSelector,
) {
	resp, err := h.handleWithRetry(r.Context(), req, targetSelector)
	if err != nil {
		logger.Errorf("Chat completion failed: %v", err)
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

// handleStream 处理流式请求
func (h *ChatHandler) handleStream(
	w http.ResponseWriter,
	r *http.Request,
	req *pkgopenai.ChatCompletionRequest,
	targetSelector router.TargetSelector,
) {
	// 设置 SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_not_supported", "Streaming not supported")
		return
	}

	// 获取所有目标用于重试
	allTargets := targetSelector.GetAll()
	var lastErr error
	
	// 首先尝试使用加权选择器选择目标
	target, err := targetSelector.Select()
	if err == nil {
		if tryErr := h.tryStreamTarget(w, r, req, flusher, target); tryErr == nil {
			return
		} else {
			logger.Warnf("Stream target %s/%s failed: %v", target.Provider, target.Model, tryErr)
			lastErr = tryErr
		}
	}

	// 如果加权选择失败，尝试所有目标（用于重试）
	for _, target := range allTargets {
		if err := h.tryStreamTarget(w, r, req, flusher, &target); err == nil {
			return
		}
		logger.Warnf("Stream target %s/%s failed: %v", target.Provider, target.Model, err)
		lastErr = err
	}

	// 所有 target 都失败
	logger.Errorf("All stream targets failed: %v", lastErr)
	writeSSEError(w, flusher, "provider_error", fmt.Sprintf("All targets failed: %v", lastErr))
}

// tryStreamTarget 尝试使用指定目标处理流式请求
func (h *ChatHandler) tryStreamTarget(
	w http.ResponseWriter,
	r *http.Request,
	req *pkgopenai.ChatCompletionRequest,
	flusher http.Flusher,
	target *config.Target,
) error {
	estimatedTokens := h.estimateTokens(req)
	backend, err := h.selectBackend(target.Provider, estimatedTokens)
	if err != nil {
		return err
	}
	
	actualUsage := 0
	defer func() {
		if bal, _ := h.balancerPool.Get(target.Provider); bal != nil {
			bal.Release(backend, actualUsage, estimatedTokens)
		}
	}()

	prov, err := h.providerPool.Get(target.Provider)
	if err != nil {
		return err
	}

	streamResp, err := prov.ChatCompletionStream(r.Context(), req, target.Model, backend.APIKey)
	if err != nil {
		if errors.IsRateLimitError(err) {
			h.markBackendUnhealthy(target.Provider, backend)
		}
		return err
	}

	// 转发流式响应
	usage, err := h.forwardStream(w, flusher, streamResp)
	if usage > 0 {
		actualUsage = usage
	} else {
		// 如果没有返回 Usage，则只能使用估算值
		// 或者我们可以在 forwardStream 中累加 output tokens
		// 这里暂且使用 estimatedTokens + usage (output) 如果 input usage 未知
		// 为了简单，如果 usage 为 0，actualUsage = estimatedTokens (input) + output_tokens (counted in forwardStream)
		// forwardStream 返回的 usage 应该是 totalTokens
		actualUsage = estimatedTokens
	}
	return nil
}

// forwardStream 转发流式响应
func (h *ChatHandler) forwardStream(w http.ResponseWriter, flusher http.Flusher, streamResp *provider.StreamResponse) (int, error) {
	stream := streamResp.Stream
	defer stream.Close()

	totalUsage := 0

	for stream.Next() {
		chunk := stream.Current()
		
		if chunk.Usage.TotalTokens > 0 {
			totalUsage = int(chunk.Usage.TotalTokens)
		}

		// DEBUG: Log if chunk has tool calls
		if len(chunk.Choices) > 0 && len(chunk.Choices[0].Delta.ToolCalls) > 0 {
			logger.Debugf("Stream chunk contains ToolCalls: %d", len(chunk.Choices[0].Delta.ToolCalls))
		}

		data, err := json.Marshal(chunk)
		if err != nil {
			continue
		}

		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	if err := stream.Err(); err != nil {
		writeSSEError(w, flusher, "stream_error", err.Error())
		return totalUsage, err
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
	
	return totalUsage, nil
}

// handleWithRetry 带重试的请求处理
func (h *ChatHandler) handleWithRetry(
	ctx context.Context,
	req *pkgopenai.ChatCompletionRequest,
	targetSelector router.TargetSelector,
) (*openai.ChatCompletion, error) {
	var lastErr error

	// 首先尝试使用加权选择器选择目标
	target, err := targetSelector.Select()
	if err == nil {
		resp, tryErr := h.tryTarget(ctx, req, target)
		if tryErr == nil {
			return resp, nil
		}
		logger.Warnf("Selected target %s/%s failed: %v", target.Provider, target.Model, tryErr)
		lastErr = tryErr
	}

	// 如果加权选择失败，尝试所有目标（用于重试）
	allTargets := targetSelector.GetAll()
	for _, target := range allTargets {
		resp, err := h.tryTarget(ctx, req, &target)
		if err == nil {
			return resp, nil
		}
		logger.Warnf("Target %s/%s failed: %v", target.Provider, target.Model, err)

		// 处理错误
		if errors.IsRateLimitError(err) {
			lastErr = err
			continue
		}

		if errors.IsRetryable(err) {
			lastErr = err
			continue
		}

		// 不可重试的错误直接返回
		return nil, err
	}

	return nil, errors.Wrap(errors.ErrCodeProviderError, "all targets failed", lastErr)
}

// tryTarget 尝试使用指定目标处理请求
func (h *ChatHandler) tryTarget(
	ctx context.Context,
	req *pkgopenai.ChatCompletionRequest,
	target *config.Target,
) (*openai.ChatCompletion, error) {
	estimatedTokens := h.estimateTokens(req)
	backend, err := h.selectBackend(target.Provider, estimatedTokens)
	if err != nil {
		return nil, err
	}

	prov, err := h.providerPool.Get(target.Provider)
	if err != nil {
		// 释放连接 (没有 usage)
		h.releaseBackend(target.Provider, backend, 0, estimatedTokens)
		return nil, err
	}

	resp, err := prov.ChatCompletion(ctx, req, target.Model, backend.APIKey)
	
	actualUsage := 0
	if resp != nil {
		actualUsage = int(resp.Usage.TotalTokens)
	}

	// 释放连接
	h.releaseBackend(target.Provider, backend, actualUsage, estimatedTokens)

	if err != nil {
		if errors.IsRateLimitError(err) {
			h.markBackendUnhealthy(target.Provider, backend)
		}
		return nil, err
	}

	return resp, nil
}

// selectBackend 选择后端
func (h *ChatHandler) selectBackend(providerName string, estimatedTokens int) (*balancer.Backend, error) {
	bal, err := h.balancerPool.Get(providerName)
	if err != nil {
		return nil, err
	}
	return bal.Select(estimatedTokens)
}

func (h *ChatHandler) releaseBackend(providerName string, backend *balancer.Backend, actualUsage, estimatedTokens int) {
	if bal, err := h.balancerPool.Get(providerName); err == nil {
		bal.Release(backend, actualUsage, estimatedTokens)
	}
}

// markBackendUnhealthy 标记后端不健康
func (h *ChatHandler) markBackendUnhealthy(providerName string, backend *balancer.Backend) {
	if bal, err := h.balancerPool.Get(providerName); err == nil {
		bal.MarkUnhealthy(backend)
	}
}

// estimateTokens 估算 token 数
func (h *ChatHandler) estimateTokens(req *pkgopenai.ChatCompletionRequest) int {
	tkm, err := tokenizer.GetEncoding(req.Model)
	if err != nil {
		// 降级到简单估算
		chars := 0
		for _, msg := range req.Messages {
			chars += len(msg.Content)
		}
		return chars/4 + len(req.Messages)*10 + 100
	}

	tokensPerMessage := 3
	tokensPerName := 1
	
	numTokens := 0
	for _, msg := range req.Messages {
		numTokens += tokensPerMessage
		numTokens += len(tkm.Encode(msg.Content, nil, nil))
		numTokens += len(tkm.Encode(msg.Role, nil, nil))
		if msg.Name != "" {
			numTokens += tokensPerName
			numTokens += len(tkm.Encode(msg.Name, nil, nil))
		}
	}
	numTokens += 3 // reply priming
	
	// 预留输出 token 缓冲
	outputBuffer := 100
	if req.MaxTokens != nil {
		// 如果指定了 MaxTokens，可以适当增加预留，但不要全部预留以免阻塞过多
		// 这里暂取较小值
		if *req.MaxTokens < 500 {
			outputBuffer = *req.MaxTokens
		} else {
			outputBuffer = 500
		}
	}
	
	return numTokens + outputBuffer
}

// writeError 写入错误响应
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(pkgopenai.ErrorResponse{
		Error: pkgopenai.ErrorDetail{
			Code:    code,
			Message: message,
			Type:    "error",
		},
	})
}

// writeSSEError 写入 SSE 错误
func writeSSEError(w http.ResponseWriter, flusher http.Flusher, code, message string) {
	errData, _ := json.Marshal(pkgopenai.ErrorResponse{
		Error: pkgopenai.ErrorDetail{
			Code:    code,
			Message: message,
			Type:    "error",
		},
	})
	fmt.Fprintf(w, "data: %s\n\n", errData)
	flusher.Flush()
}
