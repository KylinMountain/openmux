package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DiscoveredModel 发现的模型信息
type DiscoveredModel struct {
	ProviderName string
	ModelID      string
}

// Fetcher 模型发现接口
type Fetcher interface {
	FetchFreeModels(ctx context.Context, baseURL string, apiKey string) ([]DiscoveredModel, error)
}

// NewFetcher 根据 provider 名称创建对应的 Fetcher
func NewFetcher(providerName string) Fetcher {
	switch providerName {
	case "openrouter":
		return &OpenRouterFetcher{}
	case "siliconflow":
		return &SiliconFlowFetcher{}
	case "modelscope":
		return &ModelScopeFetcher{}
	default:
		return &GenericFetcher{provider: providerName}
	}
}

var httpClient = &http.Client{Timeout: 30 * time.Second}

func fetchJSON(ctx context.Context, url, apiKey string, result interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(result)
}

// --- OpenRouter ---
// 免费模型: pricing.prompt == "0" && pricing.completion == "0"

type OpenRouterFetcher struct{}

func (f *OpenRouterFetcher) FetchFreeModels(ctx context.Context, baseURL, apiKey string) ([]DiscoveredModel, error) {
	url := strings.TrimSuffix(baseURL, "/") + "/models"
	var resp OpenRouterModelsResponse
	if err := fetchJSON(ctx, url, apiKey, &resp); err != nil {
		return nil, fmt.Errorf("openrouter: %w", err)
	}

	var models []DiscoveredModel
	for _, m := range resp.Data {
		if isFreePrice(m.Pricing.Prompt) && isFreePrice(m.Pricing.Completion) {
			models = append(models, DiscoveredModel{
				ProviderName: "openrouter",
				ModelID:      m.ID,
			})
		}
	}
	return models, nil
}

// isFreePrice 判断 OpenRouter 的定价字段是否为免费
// 可能的值: "0", "free", "" 等
func isFreePrice(price string) bool {
	p := strings.TrimSpace(strings.ToLower(price))
	return p == "0" || p == "free" || p == ""
}

// --- SiliconFlow ---
// 免费模型: ID 不以 "Pro/" 开头的模型

type SiliconFlowFetcher struct{}

func (f *SiliconFlowFetcher) FetchFreeModels(ctx context.Context, baseURL, apiKey string) ([]DiscoveredModel, error) {
	url := strings.TrimSuffix(baseURL, "/") + "/models"
	var resp ModelsResponse
	if err := fetchJSON(ctx, url, apiKey, &resp); err != nil {
		return nil, fmt.Errorf("siliconflow: %w", err)
	}

	var models []DiscoveredModel
	for _, m := range resp.Data {
		if !strings.HasPrefix(m.ID, "Pro/") {
			models = append(models, DiscoveredModel{
				ProviderName: "siliconflow",
				ModelID:      m.ID,
			})
		}
	}
	return models, nil
}

// --- ModelScope ---
// 全部模型免费（每天 2000 次调用限制）

type ModelScopeFetcher struct{}

func (f *ModelScopeFetcher) FetchFreeModels(ctx context.Context, baseURL, apiKey string) ([]DiscoveredModel, error) {
	url := strings.TrimSuffix(baseURL, "/") + "/models"
	var resp ModelsResponse
	if err := fetchJSON(ctx, url, apiKey, &resp); err != nil {
		return nil, fmt.Errorf("modelscope: %w", err)
	}

	var models []DiscoveredModel
	for _, m := range resp.Data {
		models = append(models, DiscoveredModel{
			ProviderName: "modelscope",
			ModelID:      m.ID,
		})
	}
	return models, nil
}

// --- Generic ---
// 对于未知 provider，尝试标准 /models 接口，列出所有模型

type GenericFetcher struct {
	provider string
}

func (f *GenericFetcher) FetchFreeModels(ctx context.Context, baseURL, apiKey string) ([]DiscoveredModel, error) {
	url := strings.TrimSuffix(baseURL, "/") + "/models"
	var resp ModelsResponse
	if err := fetchJSON(ctx, url, apiKey, &resp); err != nil {
		return nil, fmt.Errorf("%s: %w", f.provider, err)
	}

	var models []DiscoveredModel
	for _, m := range resp.Data {
		models = append(models, DiscoveredModel{
			ProviderName: f.provider,
			ModelID:      m.ID,
		})
	}
	return models, nil
}
