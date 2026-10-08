package handler

import (
	_ "embed"
	"net/http"
)

//go:embed ui/index.html
var uiIndexHTML []byte

// UIHandler 嵌入式静态页：展示已注册路由 + OpenRouter catalog。
// 数据通过 fetch 调用 /v1/models 与 /admin/openrouter/catalog 获取。
type UIHandler struct{}

func NewUIHandler() *UIHandler { return &UIHandler{} }

func (h *UIHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(uiIndexHTML)
}
