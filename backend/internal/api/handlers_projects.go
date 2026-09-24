package api

import (
	"context"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/fsutil"
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
	full, err := fsutil.SafeJoin(s.projectRoot(proj), pf.Path)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "路径越界"})
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+pf.Name+"\"")
	http.ServeFile(w, r, full)
}

// normalizeProjectLocalDir 保存前归一化 local_dir（~ 展开 / Windows 盘符归一）。
// 非空且归一化后仍非绝对路径 → 写 400 并返回 false（防止 ~ / 相对路径原样入库，
// 导致文件视图与对话工具按错误路径寻址——REQ-101 修复：检测接口展开校验，入库必须同规则）。
func normalizeProjectLocalDir(w http.ResponseWriter, p *store.Project) bool {
	p.LocalDir = fsutil.NormalizeDir(p.LocalDir)
	if p.LocalDir != "" && !fsutil.IsAbsDir(p.LocalDir) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "local_dir 必须是绝对路径（以 / 开头，Windows 用 C:\\ 开头，或以 ~ 开头，保存时自动展开 ~）；收到: " + p.LocalDir})
		return false
	}
	return true
}

// projectRoot 项目文件作用根：绑定 local_dir 优先，否则回退 FilesRoot/{id}（REQ-101 v0.17）。
// local_dir 经归一化（~ 展开 / Windows 盘符）后使用，兼容历史未展开数据（v0.17 前入库的 ~/...）。
func (s *Server) projectRoot(p *store.Project) string {
	if p != nil && p.LocalDir != "" {
		if d := fsutil.NormalizeDir(p.LocalDir); fsutil.IsAbsDir(d) {
			return d
		}
	}
	id := ""
	if p != nil {
		id = p.ID
	}
	return filepath.Join(s.FilesRoot, id)
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
func (s *Server) validateProjectDir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dir string `json:"dir"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, err)
		return
	}
	dir := fsutil.NormalizeDir(req.Dir)
	if dir == "" || dir == "." {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "dir 必填"})
		return
	}
	if !fsutil.IsAbsDir(dir) {
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

// gitOutput 执行 git 子命令（-C dir），单次 3s 超时，返回修剪首尾空白的输出。
func gitOutput(dir string, args ...string) (string, error) {
	out, err := gitOutRaw(dir, args...)
	return strings.TrimSpace(out), err
}

// gitOutRaw 同 gitOutput，但不修剪首尾空白——porcelain 输出首行的前导空格是状态位（如 " M path"），
// 修剪会让首行 code/path 错位（REQ-102 深度版修复，gitStatusMap 同步受益）；
// -c core.quotepath=off 让中文等非 ASCII 路径原样输出（否则被 \346 八进制转义，前端匹配/展示失真）。
func gitOutRaw(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	full := append([]string{"-C", dir, "-c", "core.quotepath=off"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
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
	full, err := fsutil.SafeJoin(fsutil.NormalizeDir(proj.LocalDir), sub)
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
	full, err := fsutil.SafeJoin(fsutil.NormalizeDir(proj.LocalDir), rel)
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
// 用 gitOutRaw 取原始输出：porcelain 首行的前导空格是状态位，TrimSpace 会让首行路径错位。
func gitStatusMap(dir string) map[string]string {
	out, err := gitOutRaw(dir, "status", "--porcelain")
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

// ---- REQ-102 深度版：Git 视图（提交历史 / 分支 / 变更明细） ----

// gitPatchLimit 单次 patch 文本上限（超限报错，防大 diff 撑爆前端）。
const gitPatchLimit = 200 << 10

// gitCommit 提交历史条目。
type gitCommit struct {
	Hash    string   `json:"hash"`
	Short   string   `json:"short"`
	Author  string   `json:"author"`
	Date    string   `json:"date"`
	Subject string   `json:"subject"`
	Refs    []string `json:"refs,omitempty"`
	Merge   bool     `json:"merge,omitempty"`
}

// gitBranch 分支条目。
type gitBranch struct {
	Name        string `json:"name"`
	Current     bool   `json:"current"`
	IsRemote    bool   `json:"is_remote"`
	ShortCommit string `json:"short_commit"`
	Date        string `json:"date"`
}

// gitFileChange 变更文件（numstat；add/del 为 -1 表示二进制或合并提交无统计）。
type gitFileChange struct {
	Path string `json:"path"`
	Add  int64  `json:"add"`
	Del  int64  `json:"del"`
}

// gitWorkingFile 工作区未提交变更（porcelain 状态码 + numstat 统计）。
type gitWorkingFile struct {
	Path   string `json:"path"`
	Code   string `json:"code"`
	Staged bool   `json:"staged,omitempty"`
	Add    *int64 `json:"add,omitempty"`
	Del    *int64 `json:"del,omitempty"`
}

// loadGitProject 取项目并校验已绑定本地目录且为 git 仓库，返回归一化目录。
// 失败时已写响应，调用方直接 return。
func (s *Server) loadGitProject(w http.ResponseWriter, r *http.Request) (string, bool) {
	proj, err := s.Store.GetProject(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return "", false
	}
	if proj.LocalDir == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "项目未绑定本地目录"})
		return "", false
	}
	dir := fsutil.NormalizeDir(proj.LocalDir)
	if !fsutil.IsAbsDir(dir) || !isGitDir(dir) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "该目录不可用或不是 Git 仓库（无 .git）"})
		return "", false
	}
	return dir, true
}

// gitSafeRev 校验 git 修订参数（ref/commit）：拒绝空串以 - 开头的值（会被当成 git 选项注入）。
func gitSafeRev(v string) bool {
	return v != "" && !strings.HasPrefix(v, "-")
}

// gitProjectLog GET /api/projects/{id}/git-log?ref=<branch>&limit=50：提交历史（REQ-102 深度版）。
// ref 缺省为当前 HEAD；limit 1~200（缺省 50）。
func (s *Server) gitProjectLog(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.loadGitProject(w, r)
	if !ok {
		return
	}
	limit := 50
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 200 {
		limit = v
	}
	ref := strings.TrimSpace(r.URL.Query().Get("ref"))
	if ref != "" && !gitSafeRev(ref) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ref 不合法"})
		return
	}
	args := []string{"log", "--date=iso-strict", "--max-count=" + strconv.Itoa(limit)}
	if ref != "" {
		args = append(args, ref)
	}
	args = append(args, `--pretty=format:%H%x1f%h%x1f%an%x1f%ad%x1f%s%x1f%D%x1f%P%x1e`)
	out, err := gitOutput(dir, args...)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "git log 失败: " + err.Error()})
		return
	}
	commits := make([]gitCommit, 0, limit)
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.Split(rec, "\x1f")
		if len(f) < 7 {
			continue
		}
		c := gitCommit{Hash: f[0], Short: f[1], Author: f[2], Date: f[3], Subject: f[4], Merge: len(strings.Fields(strings.TrimSpace(f[6]))) >= 2}
		if d := strings.TrimSpace(f[5]); d != "" {
			for _, part := range strings.Split(d, ", ") {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				c.Refs = append(c.Refs, strings.TrimPrefix(part, "HEAD -> "))
			}
		}
		commits = append(commits, c)
	}
	writeJSON(w, http.StatusOK, map[string]any{"commits": commits})
}

// gitProjectBranches GET /api/projects/{id}/git-branches：本地 + 远程分支（REQ-102 深度版）。
func (s *Server) gitProjectBranches(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.loadGitProject(w, r)
	if !ok {
		return
	}
	out, err := gitOutput(dir, "for-each-ref",
		`--format=%(refname)%09%(objectname:short)%09%(creatordate:iso-strict)%09%(HEAD)`,
		"refs/heads", "refs/remotes")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "git for-each-ref 失败: " + err.Error()})
		return
	}
	branches := []gitBranch{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimSpace(line), "\t")
		if len(f) < 4 || f[0] == "" {
			continue
		}
		isRemote := strings.HasPrefix(f[0], "refs/remotes/")
		name := strings.TrimPrefix(strings.TrimPrefix(f[0], "refs/heads/"), "refs/remotes/")
		if isRemote && strings.HasSuffix(name, "/HEAD") {
			continue // 远程 HEAD 符号引用与实体分支重复，跳过
		}
		branches = append(branches, gitBranch{
			Name: name, Current: f[3] == "*", IsRemote: isRemote, ShortCommit: f[1], Date: f[2],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"branches": branches})
}

// parseNumstat 解析 numstat 输出（add\tdel\tpath；- 为二进制 → -1）。
func parseNumstat(out string) []gitFileChange {
	files := []gitFileChange{}
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 3 {
			continue
		}
		ch := gitFileChange{Path: strings.Join(f[2:], "\t")}
		n, err := strconv.ParseInt(f[0], 10, 64)
		ch.Add = map[bool]int64{true: n, false: -1}[err == nil]
		n, err = strconv.ParseInt(f[1], 10, 64)
		ch.Del = map[bool]int64{true: n, false: -1}[err == nil]
		files = append(files, ch)
	}
	return files
}

// gitProjectCommitFiles GET /api/projects/{id}/git-commit-files?commit=<sha>：单次提交变更文件明细。
func (s *Server) gitProjectCommitFiles(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.loadGitProject(w, r)
	if !ok {
		return
	}
	commit := strings.TrimSpace(r.URL.Query().Get("commit"))
	if !gitSafeRev(commit) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "commit 不合法"})
		return
	}
	out, err := gitOutput(dir, "show", "--numstat", "--format=", commit)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "提交不存在或读取失败: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": parseNumstat(out)})
}

// gitProjectCommitPatch GET /api/projects/{id}/git-commit-patch?commit=<sha>&path=<file>：
// 单提交（或提交内单文件）patch 文本，≤200KB。
func (s *Server) gitProjectCommitPatch(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.loadGitProject(w, r)
	if !ok {
		return
	}
	commit := strings.TrimSpace(r.URL.Query().Get("commit"))
	if !gitSafeRev(commit) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "commit 不合法"})
		return
	}
	args := []string{"show", "--format=", commit}
	if path := strings.TrimSpace(r.URL.Query().Get("path")); path != "" {
		if strings.HasPrefix(path, "-") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path 不合法"})
			return
		}
		args = append(args, "--", path)
	}
	out, err := gitOutput(dir, args...)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "patch 读取失败: " + err.Error()})
		return
	}
	if len(out) > gitPatchLimit {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "diff 过大（>200KB），请在本地编辑器查看"})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(out))
}

// gitProjectWorking GET /api/projects/{id}/git-working：工作区未提交变更明细（porcelain + numstat）。
func (s *Server) gitProjectWorking(w http.ResponseWriter, r *http.Request) {
	dir, ok := s.loadGitProject(w, r)
	if !ok {
		return
	}
	statusOut, err := gitOutRaw(dir, "status", "--porcelain")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "git status 失败: " + err.Error()})
		return
	}
	unstaged := map[string]gitFileChange{}
	if out, err := gitOutput(dir, "diff", "--numstat"); err == nil {
		for _, f := range parseNumstat(out) {
			unstaged[f.Path] = f
		}
	}
	staged := map[string]gitFileChange{}
	if out, err := gitOutput(dir, "diff", "--cached", "--numstat"); err == nil {
		for _, f := range parseNumstat(out) {
			staged[f.Path] = f
		}
	}
	files := []gitWorkingFile{}
	for _, line := range strings.Split(statusOut, "\n") {
		if len(line) < 4 {
			continue
		}
		code, path := line[:2], strings.TrimSpace(line[3:])
		if i := strings.Index(path, " -> "); i >= 0 { // rename 取新路径
			path = path[i+4:]
		}
		path = strings.Trim(path, `"`)
		wf := gitWorkingFile{Path: path, Code: strings.TrimSpace(code), Staged: code[0] != ' '}
		var m map[string]gitFileChange
		if code[1] != ' ' && code[1] != '?' {
			m = unstaged
		} else if wf.Staged {
			m = staged
		}
		if f, hit := m[path]; hit {
			add, del := f.Add, f.Del
			wf.Add, wf.Del = &add, &del
		}
		files = append(files, wf)
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files})
}

func timeStamp() string {
	return time.Now().Format("20060102_150405")
}
