package tool

// REQ-202/M38 Harness 执行面：文件原语工具（grep/glob/read_file/write_file 泛化）。
// 安全边界（REQ-114 绑定即授权口径的延伸）：全部操作以 DirToolDeps.Root 为唯一根，
// 路径经 fsutil.SafeJoin 约束（拒绝绝对路径/.. 与符号链接逃逸）；根由装配期解析
// （项目会话=项目文件根；非项目会话=agent.work_dir，未配置则工具不装配）。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/fsutil"
)

// grep/glob 走查上限（防大目录拖垮；如实截断并在结果中标注）。
const (
	grepMaxFiles    = 300
	grepMaxHits     = 50
	grepMaxFileLen  = 1 << 20 // 1MB 以上文件跳过
	globMaxPaths    = 200
	writeFileMaxLen = 1 << 20 // 1MB
)

// DirToolDeps 目录原语工具依赖：Root 为唯一授权根（绝对路径，装配期已归一校验）。
type DirToolDeps struct {
	Root string
}

type grepIn struct {
	Pattern string `json:"pattern" jsonschema:"required"`
	Glob    string `json:"glob,omitempty"`
}

type grepOut struct {
	Error   string `json:"error,omitempty"` // 业务错误回执（参数/路径越界/IO——文本回喂模型，不炸 run）
	Hits      []string `json:"hits"`
	Truncated bool     `json:"truncated"`
}

// NewGrepTool 只读内容检索：在授权根内按正则/字面量搜文件内容，返回 file:line:text 命中。
func NewGrepTool(deps DirToolDeps) (einotool.BaseTool, error) {
	return utils.InferTool("grep",
		"在授权目录内按正则（或字面量）搜索文件内容，返回 file:line:text 命中列表（最多 50 条）。用于定位代码/文档中的关键词。",
		func(_ context.Context, in grepIn) (*grepOut, error) {
			pat := strings.TrimSpace(in.Pattern)
			if pat == "" {
				return &grepOut{Error: "pattern 不能为空"}, nil
			}
			re, err := regexp.Compile(pat)
			if err != nil { // 正则非法降级为字面量包含（诚实容错）
				re = regexp.MustCompile(regexp.QuoteMeta(pat))
			}
			skip := regexp.MustCompile(`(^|/)\.git(/|$)`)
			var hits []string
			truncated := false
			files := 0
			_ = filepath.WalkDir(deps.Root, func(path string, d os.DirEntry, err error) error {
				if err != nil || d == nil {
					return nil
				}
				if d.IsDir() {
					if d.Name() == ".git" || d.Name() == "node_modules" {
						return filepath.SkipDir
					}
					return nil
				}
				if skip.MatchString(path) {
					return nil
				}
				if in.Glob != "" {
					if ok, gerr := filepath.Match(in.Glob, d.Name()); gerr != nil || !ok {
						return nil
					}
				}
				info, ierr := d.Info()
				if ierr != nil || info.Size() > grepMaxFileLen {
					return nil
				}
				files++
				if files > grepMaxFiles {
					truncated = true
					return filepath.SkipAll
				}
				b, rerr := os.ReadFile(path)
				if rerr != nil || !isTextual(b) {
					return nil
				}
				for i, line := range strings.Split(string(b), "\n") {
					if re.MatchString(line) {
						if len(hits) >= grepMaxHits {
							truncated = true
							return filepath.SkipAll
						}
						rel, _ := filepath.Rel(deps.Root, path)
						hits = append(hits, fmt.Sprintf("%s:%d:%s", rel, i+1, strings.TrimSpace(line)))
					}
				}
				return nil
			})
			if hits == nil {
				hits = []string{}
			}
			return &grepOut{Hits: hits, Truncated: truncated}, nil
		})
}

type globIn struct {
	Pattern string `json:"pattern" jsonschema:"required"`
}

type globOut struct {
	Error   string `json:"error,omitempty"` // 业务错误回执（参数/路径越界/IO——文本回喂模型，不炸 run）
	Paths     []string `json:"paths"`
	Truncated bool     `json:"truncated"`
}

// NewGlobTool 只读路径匹配：在授权根内按 glob 模式（如 "**/*.go" 的目录递归形态）列文件相对路径。
func NewGlobTool(deps DirToolDeps) (einotool.BaseTool, error) {
	return utils.InferTool("glob",
		"在授权目录内按模式匹配文件路径（如 *.go、data/*.json、**/*.md 递归），返回相对路径列表（最多 200 条）。用于发现文件。",
		func(_ context.Context, in globIn) (*globOut, error) {
			pat := strings.TrimSpace(in.Pattern)
			if pat == "" || strings.Contains(pat, "..") {
				return &globOut{Error: fmt.Sprintf("pattern 不合法: %q", in.Pattern)}, nil
			}
			recursive := strings.HasPrefix(pat, "**/")
			base := filepath.Join(deps.Root, filepath.FromSlash(strings.TrimPrefix(pat, "**/")))
			var hits []string
			truncated := false
			if recursive { // **/：全根递归按尾段匹配
				_, tail := filepath.Split(pat)
				_ = filepath.WalkDir(deps.Root, func(path string, d os.DirEntry, err error) error {
					if err != nil || d == nil || d.IsDir() {
						return nil
					}
					if ok, merr := filepath.Match(tail, d.Name()); merr != nil || !ok {
						return nil
					}
					if len(hits) >= globMaxPaths {
						truncated = true
						return filepath.SkipAll
					}
					rel, _ := filepath.Rel(deps.Root, path)
					hits = append(hits, filepath.ToSlash(rel))
					return nil
				})
			} else {
				matches, merr := filepath.Glob(base)
				if merr != nil {
					return &globOut{Error: fmt.Sprintf("pattern 不合法: %v", merr)}, nil
				}
				sort.Strings(matches)
				for _, m := range matches {
					if info, ierr := os.Stat(m); ierr != nil || info.IsDir() {
						continue
					}
					if len(hits) >= globMaxPaths {
						truncated = true
						break
					}
					rel, _ := filepath.Rel(deps.Root, m)
					hits = append(hits, filepath.ToSlash(rel))
				}
			}
			if hits == nil {
				hits = []string{}
			}
			return &globOut{Paths: hits, Truncated: truncated}, nil
		})
}

type writeFileIn struct {
	Path    string `json:"path" jsonschema:"required"`
	Content string `json:"content" jsonschema:"required"`
	Append  bool   `json:"append,omitempty"`
}

type writeFileOut struct {
	Error   string `json:"error,omitempty"` // 业务错误回执（参数/路径越界/IO——文本回喂模型，不炸 run）
	Path   string `json:"path"`
	Bytes  int    `json:"bytes"`
	Append bool   `json:"append"`
}

// NewWriteFileTool 写文件：在授权根内创建/覆写/追加，SafeJoin 防越界，上限 1MB。
func NewWriteFileTool(deps DirToolDeps) (einotool.BaseTool, error) {
	return utils.InferTool("write_file",
		"在授权目录内写入文本文件（默认覆盖同名；append=true 追加）。路径为目录内相对路径，禁止越界。",
		func(_ context.Context, in writeFileIn) (*writeFileOut, error) {
			rel := strings.TrimSpace(in.Path)
			if rel == "" || strings.Contains(rel, "..") {
				return &writeFileOut{Error: fmt.Sprintf("path 不合法: %q", in.Path)}, nil
			}
			if len([]rune(in.Content)) > writeFileMaxLen {
				return &writeFileOut{Error: fmt.Sprintf("内容超过 1MB 上限（%d 字符）", len([]rune(in.Content)))}, nil
			}
			full, err := safeWritePath(deps.Root, rel)
			if err != nil {
				return &writeFileOut{Error: fmt.Sprintf("路径越界: %v", err)}, nil
			}
			flag := os.O_CREATE | os.O_WRONLY
			if in.Append {
				flag |= os.O_APPEND
			} else {
				flag |= os.O_TRUNC
			}
			f, err := os.OpenFile(full, flag, 0o644)
			if err != nil {
				return &writeFileOut{Error: err.Error()}, nil
			}
			defer f.Close()
			n, err := f.WriteString(in.Content)
			if err != nil {
				return &writeFileOut{Error: err.Error()}, nil
			}
			relOut, _ := filepath.Rel(deps.Root, full)
			return &writeFileOut{Path: filepath.ToSlash(relOut), Bytes: n, Append: in.Append}, nil
		})
}

// NewDirReadFileTool 读文件（work_dir 泛化形态，无项目文件登记依赖）：授权根内 SafeJoin 读取，上限 1MB。
func NewDirReadFileTool(deps DirToolDeps) (einotool.BaseTool, error) {
	return utils.InferTool("read_file",
		"读取授权目录内的文本文件（相对路径，上限 1MB）。配合 grep/glob 先定位再精读。",
		func(_ context.Context, in readFileIn) (*readFileOut, error) {
			rel := strings.TrimSpace(in.Path)
			if rel == "" || strings.Contains(rel, "..") {
				return &readFileOut{Error: fmt.Sprintf("path 不合法: %q", in.Path)}, nil
			}
			full, err := fsutil.SafeJoin(deps.Root, filepath.FromSlash(rel))
			if err != nil {
				return &readFileOut{Error: fmt.Sprintf("路径越界: %v", err)}, nil
			}
			b, rerr := os.ReadFile(full)
			if rerr != nil {
				return &readFileOut{Error: rerr.Error()}, nil
			}
			if len(b) > grepMaxFileLen {
				b = b[:grepMaxFileLen]
			}
			relOut, _ := filepath.Rel(deps.Root, full)
			return &readFileOut{Path: filepath.ToSlash(relOut), Content: string(b)}, nil
		})
}

// safeWritePath 写路径安全解析：词法约束（相对/无 ..）→ 建父目录（仅根内）→ 实路径符号链接
// 逃逸校验（SafeJoin 对「父目录尚不存在」的新建路径会拒绝，写场景先建目录再验实路径）。
func safeWritePath(root, rel string) (string, error) {
	rel = filepath.FromSlash(strings.TrimSpace(rel))
	if rel == "" || filepath.IsAbs(rel) {
		return "", fsutil.ErrPathOutside
	}
	if c := filepath.Clean(rel); c == ".." || strings.HasPrefix(c, ".."+string(os.PathSeparator)) {
		return "", fsutil.ErrPathOutside
	}
	full := filepath.Join(root, rel)
	if !fsutil.Within(root, full) {
		return "", fsutil.ErrPathOutside
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	realDir, err := filepath.EvalSymlinks(filepath.Dir(full))
	if err != nil {
		return "", err
	}
	if !fsutil.Within(realRoot, realDir) {
		return "", fsutil.ErrPathOutside
	}
	return full, nil
}

// isTextual 粗判文本（含 NUL 即按二进制跳过）。
func isTextual(b []byte) bool {
	limit := len(b)
	if limit > 8000 {
		limit = 8000
	}
	for _, c := range b[:limit] {
		if c == 0 {
			return false
		}
	}
	return true
}
