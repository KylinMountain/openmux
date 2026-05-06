package discovery

// ModelsResponse 标准 OpenAI /v1/models 响应格式
// 适用于 SiliconFlow、ModelScope 等
type ModelsResponse struct {
	Object string       `json:"object"`
	Data   []ModelEntry `json:"data"`
}

// ModelEntry 标准模型条目
type ModelEntry struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// OpenRouterModelsResponse OpenRouter 的 /v1/models 响应
type OpenRouterModelsResponse struct {
	Data []OpenRouterModelEntry `json:"data"`
}

// OpenRouterModelEntry OpenRouter 模型条目（包含定价信息）
type OpenRouterModelEntry struct {
	ID      string              `json:"id"`
	Name    string              `json:"name"`
	Pricing OpenRouterPricing   `json:"pricing"`
}

// OpenRouterPricing OpenRouter 定价信息
type OpenRouterPricing struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
}
