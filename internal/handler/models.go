package handler

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/openmux/openmux/internal/router"
)

// ModelsHandler 模型列表处理器
type ModelsHandler struct {
	router *router.Router
}

// NewModelsHandler 创建模型处理器
func NewModelsHandler(router *router.Router) *ModelsHandler {
	return &ModelsHandler{
		router: router,
	}
}

// modelEntry 是 OpenAI /v1/models 标准字段加上 openmux 扩展字段。
// 客户端如果只关心 id/object/created/owned_by 仍然能正常解析；UI 可以读 openmux 子对象。
type modelEntry struct {
	ID      string                `json:"id"`
	Object  string                `json:"object"`
	Created int64                 `json:"created"`
	OwnedBy string                `json:"owned_by"`
	OpenMux *router.ModelMetadata `json:"openmux,omitempty"`
}

type modelList struct {
	Object string       `json:"object"`
	Data   []modelEntry `json:"data"`
}

// Handle 处理模型列表请求
func (h *ModelsHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}

	infos := h.router.ListModelsWithMetadata()
	sort.Slice(infos, func(i, j int) bool { return infos[i].Name < infos[j].Name })

	now := time.Now().Unix()
	out := modelList{
		Object: "list",
		Data:   make([]modelEntry, 0, len(infos)),
	}

	for _, info := range infos {
		entry := modelEntry{
			ID:      info.Name,
			Object:  "model",
			Created: now,
			OwnedBy: "openmux",
		}
		// 静态路由没有 metadata 时也补一个 source=static，方便 UI 区分。
		if info.HasMeta {
			meta := info.Metadata
			entry.OpenMux = &meta
		} else if info.IsStatic {
			entry.OpenMux = &router.ModelMetadata{Source: "static"}
		}
		out.Data = append(out.Data, entry)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}
