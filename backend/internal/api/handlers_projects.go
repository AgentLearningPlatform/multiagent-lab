package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	proj, err := s.Store.GetProject(pid)
	if err != nil {
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
	dir := s.projectRoot(proj)
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
	proj, err := s.Store.GetProject(pf.ProjectID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "project not found"})
		return
	}
	full, err := safeJoin(s.projectRoot(proj), pf.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径越界"})
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+pf.Name+"\"")
	http.ServeFile(w, r, full)
}

// projectRoot 项目文件作用根：绑定 local_dir 优先，否则回退 FilesRoot/{id}（REQ-101 v0.17）。
func (s *Server) projectRoot(p *store.Project) string {
	if p != nil && p.LocalDir != "" {
		return p.LocalDir
	}
	id := ""
	if p != nil {
		id = p.ID
	}
	return filepath.Join(s.FilesRoot, id)
}

// errPathOutside 路径越界（目录穿越 / 符号链接逃逸）。
var errPathOutside = errors.New("path outside root")

// within 判断 p 是否等于 root 或位于 root 之内（词法层面）。
func within(root, p string) bool {
	root = filepath.Clean(root)
	p = filepath.Clean(p)
	if root == string(os.PathSeparator) {
		return strings.HasPrefix(p, string(os.PathSeparator))
	}
	return p == root || strings.HasPrefix(p, root+string(os.PathSeparator))
}

// safeJoin 将相对路径 rel 安全拼接到 root：拒绝绝对路径与 ..，并做符号链接逃逸校验。
func safeJoin(root, rel string) (string, error) {
	root = filepath.Clean(root)
	if rel == "" || rel == "." {
		return root, nil
	}
	if filepath.IsAbs(rel) {
		return "", errPathOutside
	}
	cleanRel := filepath.Clean(rel)
	if cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(os.PathSeparator)) {
		return "", errPathOutside
	}
	full := filepath.Join(root, cleanRel)
	if !within(root, full) {
		return "", errPathOutside
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	if realFull, err := filepath.EvalSymlinks(full); err == nil {
		if !within(realRoot, realFull) {
			return "", errPathOutside
		}
	} else {
		// 目标不存在：校验父目录实路径 + basename，防经符号链接父目录逃逸
		realParent, perr := filepath.EvalSymlinks(filepath.Dir(full))
		if perr != nil {
			return "", errPathOutside
		}
		if !within(realRoot, filepath.Join(realParent, filepath.Base(full))) {
			return "", errPathOutside
		}
	}
	return full, nil
}

// ---- REQ-101 项目绑定本地目录 ----

type validateDirResp struct {
	Exists    bool   `json:"exists"`
	IsDir     bool   `json:"is_dir"`
	IsGit     bool   `json:"is_git"`
	GitBranch string `json:"git_branch"`
	GitCommit string `json:"git_commit"`
	GitDirty  bool   `json:"git_dirty"`
	Error     string `json:"error,omitempty"`
}

// validateProjectDir POST /api/projects/validate-dir：校验本地目录与 git 状态（REQ-101）。
// isWindowsPath 识别 Windows 盘符路径形态（C:/ 或 C:\，大小写盘符均可）。
// 用于跨平台场景：Linux 运行时接受 Windows 客户端提交的本地目录（如挂载盘）。
func isWindowsPath(p string) bool {
	if len(p) < 3 {
		return false
	}
	c := p[0]
	return (c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') && p[1] == ':' && (p[2] == '\\' || p[2] == '/')
}

func (s *Server) validateProjectDir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir string `json:"dir"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	dir := strings.TrimSpace(req.Dir)
	if dir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dir 必填"})
		return
	}
	// ~ 前缀展开（macOS/Linux 输入习惯；Windows 盘符形态不受影响）
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(dir, "~"), "/"))
		}
	}
	// Windows 路径支持：filepath.IsAbs 在 Linux 运行时对 `C:\...` 返回 false，
	// 显式识别盘符形态（C:/ 或 C:\，含正斜杠变体），归一为运行时格式后校验。
	if isWindowsPath(dir) {
		dir = filepath.FromSlash(dir)
	}
	if !filepath.IsAbs(dir) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dir 必须是绝对路径（以 / 开头，Windows 用 C:\\ 开头，或以 ~ 开头）；收到: " + req.Dir})
		return
	}
	resp := validateDirResp{}
	fi, err := os.Stat(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			resp.Error = err.Error()
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.Exists = true
	resp.IsDir = fi.IsDir()
	if !resp.IsDir {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if _, err := exec.LookPath("git"); err != nil {
		resp.Error = "git 命令不可用"
		writeJSON(w, http.StatusOK, resp)
		return
	}
	// .git 存在（worktree 下为文件）视为 git 仓库
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.IsGit = true
	if out, err := gitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD"); err == nil {
		resp.GitBranch = out
	} else {
		resp.Error = err.Error()
	}
	if out, err := gitOutput(dir, "rev-parse", "--short", "HEAD"); err == nil {
		resp.GitCommit = out
	} else if resp.Error == "" {
		resp.Error = err.Error()
	}
	if out, err := gitOutput(dir, "status", "--porcelain"); err == nil {
		resp.GitDirty = strings.TrimSpace(out) != ""
	} else if resp.Error == "" {
		resp.Error = err.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

// gitOutput 执行 git 子命令（-C dir），单次 3s 超时。
func gitOutput(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ---- REQ-102 文件视图 API（项目目录文件列表） ----

type dirFileEntry struct {
	Name      string `json:"name"`
	IsDir     bool   `json:"is_dir"`
	Size      int64  `json:"size"`
	ModTime   string `json:"mod_time"`
	GitStatus string `json:"git_status,omitempty"`
}

// listDirFiles GET /api/projects/{id}/dir-files?path=<sub>：单层列出绑定目录内容（REQ-102）。
func (s *Server) listDirFiles(w http.ResponseWriter, r *http.Request) {
	proj, err := s.Store.GetProject(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if proj.LocalDir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "项目未绑定本地目录"})
		return
	}
	sub := strings.TrimSpace(r.URL.Query().Get("path"))
	full, err := safeJoin(proj.LocalDir, sub)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径越界"})
		return
	}
	des, err := os.ReadDir(full)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "目录不存在或不可读"})
		return
	}
	status := map[string]string{}
	if isGitDir(proj.LocalDir) {
		status = gitStatusMap(proj.LocalDir)
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
		if gs, ok := status[gitRelKey(sub, de.Name())]; ok {
			e.GitStatus = gs
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
	writeJSON(w, http.StatusOK, map[string]any{"path": relOut, "entries": entries})
}

// getDirFile GET /api/projects/{id}/dir-file?path=<rel>：读取绑定目录内文件文本预览（REQ-102）。
func (s *Server) getDirFile(w http.ResponseWriter, r *http.Request) {
	proj, err := s.Store.GetProject(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if proj.LocalDir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "项目未绑定本地目录"})
		return
	}
	rel := strings.TrimSpace(r.URL.Query().Get("path"))
	if rel == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path 必填"})
		return
	}
	full, err := safeJoin(proj.LocalDir, rel)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径越界"})
		return
	}
	fi, err := os.Stat(full)
	if err != nil || fi.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "文件不存在"})
		return
	}
	if fi.Size() > 1<<20 { // 1MB 预览上限
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

// isGitDir 判断目录是否为 git 仓库根（.git 目录或 worktree 的 .git 文件）。
func isGitDir(dir string) bool {
	if _, err := exec.LookPath("git"); err != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// gitRelKey 计算条目在 git status 中的相对路径键。
func gitRelKey(sub, name string) string {
	if sub == "" || sub == "." {
		return name
	}
	return filepath.ToSlash(filepath.Join(sub, name))
}

// gitStatusMap 单次 git status --porcelain，映射为 modified/added/untracked/deleted。
func gitStatusMap(dir string) map[string]string {
	out, err := gitOutput(dir, "status", "--porcelain")
	if err != nil {
		return map[string]string{}
	}
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 4 {
			continue
		}
		code := line[:2]
		p := line[3:]
		if i := strings.Index(p, " -> "); i >= 0 { // rename：取新路径
			p = p[i+4:]
		}
		p = strings.Trim(p, "\"")
		p = strings.TrimSuffix(p, "/") // 未跟踪目录以 / 结尾
		var st string
		switch {
		case code == "??":
			st = "untracked"
		case strings.Contains(code, "D"):
			st = "deleted"
		case strings.Contains(code, "A"):
			st = "added"
		case strings.Contains(code, "M"):
			st = "modified"
		default:
			continue
		}
		m[filepath.ToSlash(p)] = st
	}
	return m
}

// contentTypeOf 按扩展名推断文本预览 Content-Type。
func contentTypeOf(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".json":
		return "application/json; charset=utf-8"
	case ".md", ".markdown":
		return "text/markdown; charset=utf-8"
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".csv":
		return "text/csv; charset=utf-8"
	case ".xml":
		return "application/xml; charset=utf-8"
	case ".ts", ".tsx", ".jsx", ".yaml", ".yml", ".txt", ".log", ".go", ".py",
		".java", ".c", ".h", ".cpp", ".rs", ".sh", ".toml", ".ini", ".conf":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func timeStamp() string {
	return time.Now().Format("20060102_150405")
}
