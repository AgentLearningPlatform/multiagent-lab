package api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// ---- 项目文件（M11 §6.13：上传/列表/下载，存储 data/projects/{project_id}/）----

// listProjectFiles GET /api/projects/{id}/files
func (s *Server) listProjectFiles(w http.ResponseWriter, r *http.Request) {
	fs, err := s.Store.ListProjectFiles(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if fs == nil {
		fs = []*store.ProjectFile{}
	}
	writeJSON(w, http.StatusOK, fs)
}

// uploadProjectFile POST /api/projects/{id}/files（multipart 字段 file）
func (s *Server) uploadProjectFile(w http.ResponseWriter, r *http.Request) {
	pid := r.PathValue("id")
	if _, err := s.Store.GetProject(pid); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil { // 32MB 上限
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart: " + err.Error()})
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing file field"})
		return
	}
	defer f.Close()

	name := filepath.Base(hdr.Filename)
	if name == "" || name == "." || strings.Contains(name, "..") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid filename"})
		return
	}
	dir := filepath.Join(s.FilesRoot, pid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeErr(w, err)
		return
	}
	rel := name
	full := filepath.Join(dir, rel)
	if _, err := os.Stat(full); err == nil { // 同名加时间戳
		ext := filepath.Ext(name)
		rel = strings.TrimSuffix(name, ext) + "_" + timeStamp() + ext
		full = filepath.Join(dir, rel)
	}
	dst, err := os.Create(full)
	if err != nil {
		writeErr(w, err)
		return
	}
	n, err := io.Copy(dst, f)
	_ = dst.Close()
	if err != nil {
		writeErr(w, err)
		return
	}
	pf := &store.ProjectFile{ProjectID: pid, Name: name, Path: rel, Size: n, Mime: hdr.Header.Get("Content-Type"), Source: "upload"}
	id, err := s.Store.InsertProjectFile(pf)
	if err != nil {
		writeErr(w, err)
		return
	}
	pf.ID = id
	writeJSON(w, http.StatusCreated, pf)
}

// downloadProjectFile GET /api/projects/{id}/files/{fid}/content
func (s *Server) downloadProjectFile(w http.ResponseWriter, r *http.Request) {
	pf, err := s.Store.GetProjectFile(r.PathValue("fid"))
	if err != nil || pf == nil || pf.ProjectID != r.PathValue("id") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "file not found"})
		return
	}
	full := filepath.Join(s.FilesRoot, pf.ProjectID, pf.Path)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+pf.Name+"\"")
	http.ServeFile(w, r, full)
}

func timeStamp() string {
	return time.Now().Format("20060102_150405")
}
