// REQ-218④/M49：智能体文件视图——以 agent.work_dir 为安全根浏览（沿项目 dir-files 先例，
// fsutil.SafeJoin 防越界；单层列表 + 1MB 文本预览）。「设为工作目录」走既有 PUT /api/agents/{id}
// 全量载荷（agentFullPayload），不另设端点；装配期 resolveWorkRoot 随新值生效=新 Run 文件工具安全根切换。
package api

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/fsutil"
)

// listAgentDirFiles GET /api/agents/{id}/dir-files?path=<sub>：以 work_dir 为根单层列出。
func (s *Server) listAgentDirFiles(w http.ResponseWriter, r *http.Request) {
	a, err := s.Store.GetAgent(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(a.WorkDir) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "智能体未设置工作目录（侧板「文件」视图可设为工作目录，或 Harness 页签填写）"})
		return
	}
	sub := strings.TrimSpace(r.URL.Query().Get("path"))
	full, err := fsutil.SafeJoin(fsutil.NormalizeDir(a.WorkDir), sub)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径越界"})
		return
	}
	des, err := os.ReadDir(full)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "目录不存在或不可读（检查工作目录是否有效）"})
		return
	}
	entries := make([]dirFileEntry, 0, len(des))
	for _, de := range des {
		info, ierr := de.Info()
		if ierr != nil {
			continue
		}
		e := dirFileEntry{Name: de.Name(), IsDir: de.IsDir(), Size: info.Size(), ModTime: info.ModTime().UTC().Format(time.RFC3339)}
		if de.IsDir() {
			e.Size = 0
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir // 目录优先
		}
		return entries[i].Name < entries[j].Name
	})
	relOut := filepath.ToSlash(filepath.Clean(sub))
	if sub == "" {
		relOut = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": relOut, "root": fsutil.NormalizeDir(a.WorkDir), "entries": entries})
}

// getAgentDirFile GET /api/agents/{id}/dir-file?path=<rel>：读取 work_dir 内文件文本预览（1MB 上限）。
func (s *Server) getAgentDirFile(w http.ResponseWriter, r *http.Request) {
	a, err := s.Store.GetAgent(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if strings.TrimSpace(a.WorkDir) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "智能体未设置工作目录"})
		return
	}
	rel := strings.TrimSpace(r.URL.Query().Get("path"))
	if rel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path 必填"})
		return
	}
	full, err := fsutil.SafeJoin(fsutil.NormalizeDir(a.WorkDir), rel)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径越界"})
		return
	}
	fi, err := os.Stat(full)
	if err != nil || fi.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "文件不存在"})
		return
	}
	if fi.Size() > 1<<20 { // 1MB 预览上限（沿项目口径）
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "文件过大，请用本地编辑器打开"})
		return
	}
	b, err := os.ReadFile(full)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", contentTypeOf(full))
	_, _ = w.Write(b)
}
