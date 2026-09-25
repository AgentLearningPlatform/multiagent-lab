// REQ-140 内部方案文档点击查看：只读服务白名单目录下的 Markdown
// （白名单目录 + .md 后缀，fsutil 防目录穿越）。前端弹层渲染（不要求在线编辑）。
// REQ-150 扩展（2026-09-25）：白名单由 docs/ 扩至 docs/ + research/。
// REQ-161 扩展（2026-09-25）：白名单扩至 docs/ + research/ + platform-knowledge/——
// 平台知识页（原参考资料中心）目录化后，文档互引相对路径点击查看覆盖平台知识目录。
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
	// 白名单目录：docs/（方案/需求事实源）+ research/（立项依据层）；fsutil 双重防越界
	clean := filepath.ToSlash(filepath.Clean(rel))
	const docsPrefix = "docs/"
	const researchPrefix = "research/"
	const knowledgePrefix = "platform-knowledge/"
	var root, sub string
	switch {
	case strings.HasPrefix(clean, docsPrefix):
		root, sub = s.DocsRoot, strings.TrimPrefix(clean, docsPrefix)
	case strings.HasPrefix(clean, researchPrefix):
		root, sub = s.ResearchRoot, strings.TrimPrefix(clean, researchPrefix)
	case strings.HasPrefix(clean, knowledgePrefix):
		root, sub = s.KnowledgeRoot, strings.TrimPrefix(clean, knowledgePrefix)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "仅支持 docs/、research/ 或 platform-knowledge/ 目录下的文档"})
		return
	}
	if strings.Contains(clean, "..") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径越界"})
		return
	}
	abs, err := fsutil.SafeJoin(root, sub)
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

