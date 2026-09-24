// REQ-140 内部方案文档点击查看：只读服务 docs/ 目录下的 Markdown（白名单目录 + .md 后缀，
// fsutil 防目录穿越）。前端弹层渲染（不要求在线编辑）。
package api

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/fsutil"
)

// docRead GET /api/docs/read?path=docs/01_智能体_需求文档_PRD.md
// → {title, content}（纯文本 Markdown，前端渲染）。越界/非 md → 400。
func (s *Server) docRead(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimSpace(r.URL.Query().Get("path"))
	if rel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path 必填（如 docs/01_xxx.md）"})
		return
	}
	if !strings.HasSuffix(rel, ".md") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "仅支持 .md 文档"})
		return
	}
	// 只允许 docs/ 前缀（内部方案文档范围）
	clean := filepath.ToSlash(filepath.Clean(rel))
	if !strings.HasPrefix(clean, "docs/") || strings.Contains(clean, "..") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "仅支持 docs/ 目录下的文档"})
		return
	}
	abs, err := fsutil.SafeJoin(s.DocsRoot, strings.TrimPrefix(clean, "docs/"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径越界"})
		return
	}
	b, rerr := os.ReadFile(abs)
	if rerr != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "文档不存在或不可读: " + rerr.Error()})
		return
	}
	title := strings.TrimPrefix(filepath.Base(clean), "docs/")
	writeJSON(w, http.StatusOK, map[string]any{
		"path":    clean,
		"title":   strings.TrimSuffix(title, ".md"),
		"content": string(b),
	})
}

