package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// HFClient HuggingFace 公开 API 客户端，用于按 repo id 查询模型参数量。
// 匿名调用，结果在进程内缓存（discovery 间隔通常 24h，缓存避免重复抓取）。
type HFClient struct {
	httpClient *http.Client
	cache      sync.Map // key: hf_id, value: hfCacheEntry
}

type hfCacheEntry struct {
	sizeB        float64
	pipelineTag  string
	fetchedAt    time.Time
}

type hfModelResponse struct {
	PipelineTag string `json:"pipeline_tag"`
	Safetensors struct {
		Total int64 `json:"total"`
	} `json:"safetensors"`
}

// HFCacheTTL HF 元数据缓存有效期。设得比 discovery interval 略长即可。
const HFCacheTTL = 7 * 24 * time.Hour

func NewHFClient() *HFClient {
	return &HFClient{
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// FetchModelInfo 查询 hf_id 对应模型的参数量（B）和 pipeline_tag。
// 失败返回 (0, "", err)，调用方应当容忍此错误。
func (c *HFClient) FetchModelInfo(ctx context.Context, hfID string) (sizeB float64, pipelineTag string, err error) {
	hfID = strings.TrimSpace(hfID)
	if hfID == "" {
		return 0, "", fmt.Errorf("empty hf id")
	}

	if cached, ok := c.cache.Load(hfID); ok {
		entry := cached.(hfCacheEntry)
		if time.Since(entry.fetchedAt) < HFCacheTTL {
			return entry.sizeB, entry.pipelineTag, nil
		}
	}

	url := "https://huggingface.co/api/models/" + hfID
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("hf api %s: HTTP %d", hfID, resp.StatusCode)
	}

	var body hfModelResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, "", err
	}

	sizeB = float64(body.Safetensors.Total) / 1e9
	pipelineTag = body.PipelineTag

	c.cache.Store(hfID, hfCacheEntry{
		sizeB:       sizeB,
		pipelineTag: pipelineTag,
		fetchedAt:   time.Now(),
	})
	return sizeB, pipelineTag, nil
}
