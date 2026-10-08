package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/openmux/openmux/internal/config"
	"github.com/openmux/openmux/internal/discovery"
)

// CatalogCacheTTL OpenRouter catalog 在内存中的缓存时间。每次 UI 打开都拉一次太浪费。
const CatalogCacheTTL = 10 * time.Minute

// OpenRouterCatalogHandler 提供 OpenRouter 全量 catalog（含付费），并附上若启用 discovery 会被
// 分到哪个 tier 的预测，方便用户对照 /v1/models 看哪些已经被收录。
type OpenRouterCatalogHandler struct {
	cfg *config.Config

	mu        sync.Mutex
	cache     *catalogResponse
	cachedAt  time.Time
	hf        *discovery.HFClient
}

func NewOpenRouterCatalogHandler(cfg *config.Config) *OpenRouterCatalogHandler {
	h := &OpenRouterCatalogHandler{cfg: cfg}
	if cfg.Discovery.FetchHuggingFaceMetadata {
		h.hf = discovery.NewHFClient()
	}
	return h
}

type catalogEntry struct {
	ID            string  `json:"id"`
	Name          string  `json:"name,omitempty"`
	CanonicalSlug string  `json:"canonical_slug,omitempty"`
	HuggingFaceID string  `json:"hugging_face_id,omitempty"`
	Modality      string  `json:"modality,omitempty"`
	ContextLength int     `json:"context_length,omitempty"`
	PricePrompt   string  `json:"price_prompt"`
	PriceCompl    string  `json:"price_completion"`
	IsFree        bool    `json:"is_free"`
	IsTextGen     bool    `json:"is_text_generation"`
	WouldBeTier   string  `json:"would_be_tier,omitempty"` // 启用 discovery 后会被归到哪个 tier
	SizeB         float64 `json:"size_b,omitempty"`
	SizeSource    string  `json:"size_source,omitempty"` // "name" / "hf-api" / "unknown"
}

type catalogResponse struct {
	Object    string         `json:"object"`
	FetchedAt time.Time      `json:"fetched_at"`
	Source    string         `json:"source"` // "live" 或 "cache"
	Total     int            `json:"total"`
	FreeCount int            `json:"free_count"`
	Data      []catalogEntry `json:"data"`
}

// OpenRouterEntry 是为了让响应里可包含原始 context_length，扩展 response.go 不太合适，
// 这里复用 fetcher 已有的 OpenRouterModelEntry 即可（虽然没 context_length 字段，需要补）。

func (h *OpenRouterCatalogHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	provCfg, ok := h.cfg.Providers["openrouter"]
	if !ok {
		writeError(w, http.StatusNotFound, "provider_not_configured", "openrouter provider not configured")
		return
	}

	resp, err := h.getCatalog(r.Context(), provCfg)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *OpenRouterCatalogHandler) getCatalog(ctx context.Context, provCfg config.ProviderConfig) (*catalogResponse, error) {
	h.mu.Lock()
	if h.cache != nil && time.Since(h.cachedAt) < CatalogCacheTTL {
		cached := *h.cache
		cached.Source = "cache"
		h.mu.Unlock()
		return &cached, nil
	}
	h.mu.Unlock()

	apiKey := ""
	if len(provCfg.APIKeys) > 0 {
		apiKey = provCfg.APIKeys[0]
	}

	fetcher := &discovery.OpenRouterFetcher{}
	raw, err := fetcher.FetchAll(ctx, provCfg.BaseURL, apiKey)
	if err != nil {
		return nil, err
	}

	resp := &catalogResponse{
		Object:    "list",
		FetchedAt: time.Now(),
		Source:    "live",
		Total:     len(raw.Data),
		Data:      make([]catalogEntry, 0, len(raw.Data)),
	}

	for _, m := range raw.Data {
		isFree := isFreePrice(m.Pricing.Prompt) && isFreePrice(m.Pricing.Completion)
		if isFree {
			resp.FreeCount++
		}
		isText := discovery.IsTextGeneration(m.Architecture.Modality)

		entry := catalogEntry{
			ID:            m.ID,
			Name:          m.Name,
			CanonicalSlug: m.CanonicalSlug,
			HuggingFaceID: m.HuggingFaceID,
			Modality:      m.Architecture.Modality,
			ContextLength: m.ContextLength,
			PricePrompt:   m.Pricing.Prompt,
			PriceCompl:    m.Pricing.Completion,
			IsFree:        isFree,
			IsTextGen:     isText,
		}

		// 仅对会被 discovery 收录的（免费 + 聊天）模型预测 tier，避免对付费模型也调 HF API。
		if isFree && isText {
			size := discovery.ParseModelSize(m.ID, m.CanonicalSlug, m.HuggingFaceID)
			source := "name"
			if size == 0 && h.hf != nil && m.HuggingFaceID != "" {
				if hfSize, _, err := h.hf.FetchModelInfo(ctx, m.HuggingFaceID); err == nil && hfSize > 0 {
					size = hfSize
					source = "hf-api"
				} else {
					source = "unknown"
				}
			} else if size == 0 {
				source = "unknown"
			}
			cap := discovery.ParseModelCapability(m.ID)
			tier := discovery.ClassifyTier(size, cap)
			entry.SizeB = size
			entry.SizeSource = source
			entry.WouldBeTier = string(tier)
		}

		resp.Data = append(resp.Data, entry)
	}

	sort.Slice(resp.Data, func(i, j int) bool { return resp.Data[i].ID < resp.Data[j].ID })

	h.mu.Lock()
	h.cache = resp
	h.cachedAt = time.Now()
	h.mu.Unlock()

	return resp, nil
}

// isFreePrice 与 discovery 包同名函数行为一致；在 handler 包中重复声明避免循环依赖。
func isFreePrice(p string) bool {
	switch p {
	case "0", "free", "":
		return true
	}
	return false
}
