// list_files / read_file 内置工具（REQ-101/102）：项目会话中大模型浏览与读取绑定的本地目录。
// 与 save_file / 文件视图 API 同一根解析规则（local_dir 优先且 ~ 展开，否则 FilesRoot/{projectID}），
// 路径一律限项目根内（fsutil.SafeJoin：拒绝绝对路径 / .. / 符号链接逃逸）。
package tool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/fsutil"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// ProjectDirDeps 项目目录工具的会话级依赖（装配期注入，项目会话才启用）。
type ProjectDirDeps struct {
	Store     *store.Store
	ProjectID string // 项目归属（必填，空则工具不可用）
	FilesRoot string // 项目文件根目录回退（如 ./data/projects）
}

// ProjectDirRoot 解析项目文件根：绑定 local_dir（~ 展开）优先，否则回退 FilesRoot/{projectID}。
// 与 save_file、文件视图 API（projectRoot）同一规则；未绑定且 FilesRoot 为空时报错。
func ProjectDirRoot(deps ProjectDirDeps) (string, error) {
	if deps.Store == nil || deps.ProjectID == "" {
		return "", fmt.Errorf("project dir: store/project required")
	}
	root := filepath.Join(deps.FilesRoot, deps.ProjectID)
	if p, err := deps.Store.GetProject(deps.ProjectID); err == nil && p != nil && p.LocalDir != "" {
		if d := fsutil.NormalizeDir(p.LocalDir); filepath.IsAbs(d) {
			return d, nil
		}
	}
	if deps.FilesRoot == "" {
		return "", fmt.Errorf("project dir: files root required")
	}
	return root, nil
}

// ---- list_files ----

type listFilesIn struct {
	Path string `json:"path,omitempty" jsonschema_description:"项目目录内相对子路径（缺省为根目录）"`
}

type listFilesEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

type listFilesOut struct {
	Path    string           `json:"path" jsonschema_description:"实际列出的相对路径（根目录为空串）"`
	Entries []listFilesEntry `json:"entries"`
}

// NewListFilesTool 构造会话级 list_files 工具：列出项目目录（或子目录）一层内容。
func NewListFilesTool(deps ProjectDirDeps) (einotool.BaseTool, error) {
	root, err := ProjectDirRoot(deps)
	if err != nil {
		return nil, err
	}
	return utils.InferTool("list_files",
		"列出项目目录下的文件与子目录（一层）。需要浏览项目已有文件、产物或代码时调用；path 缺省为项目根目录。",
		func(_ context.Context, in listFilesIn) (*listFilesOut, error) {
			sub := strings.TrimSpace(in.Path)
			full, err := fsutil.SafeJoin(root, sub)
			if err != nil {
				return nil, fmt.Errorf("路径越界: %q", in.Path)
			}
			des, err := os.ReadDir(full)
			if err != nil {
				return nil, fmt.Errorf("目录不存在或不可读: %q", sub)
			}
			entries := make([]listFilesEntry, 0, len(des))
			for _, de := range des {
				e := listFilesEntry{Name: de.Name(), IsDir: de.IsDir()}
				if !de.IsDir() {
					if info, ierr := de.Info(); ierr == nil {
						e.Size = info.Size()
					}
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
			if sub == "" || sub == "." {
				relOut = ""
			}
			return &listFilesOut{Path: relOut, Entries: entries}, nil
		})
}

// ---- read_file ----

// maxReadFileSize 单次读取上限（与文件视图预览一致：1MB）。
const maxReadFileSize = 1 << 20

type readFileIn struct {
	Path string `json:"path" jsonschema_description:"项目目录内相对路径（指向文件）"`
}

type readFileOut struct {
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	Content string `json:"content"`
}

// NewReadFileTool 构造会话级 read_file 工具：读取项目目录内文本文件（≤1MB）。
func NewReadFileTool(deps ProjectDirDeps) (einotool.BaseTool, error) {
	root, err := ProjectDirRoot(deps)
	if err != nil {
		return nil, err
	}
	return utils.InferTool("read_file",
		"读取项目目录内文本文件的内容（≤1MB）。需要查看对话产物、代码或数据文件时调用；path 为项目目录内相对路径。",
		func(_ context.Context, in readFileIn) (*readFileOut, error) {
			rel := strings.TrimSpace(in.Path)
			if rel == "" {
				return nil, fmt.Errorf("path 必填")
			}
			full, err := fsutil.SafeJoin(root, rel)
			if err != nil {
				return nil, fmt.Errorf("路径越界: %q", in.Path)
			}
			fi, err := os.Stat(full)
			if err != nil {
				return nil, fmt.Errorf("文件不存在: %q", rel)
			}
			if fi.IsDir() {
				return nil, fmt.Errorf("%q 是目录，请用 list_files 列出内容", rel)
			}
			if fi.Size() > maxReadFileSize {
				return nil, fmt.Errorf("文件过大（%d 字节，上限 1MB），无法一次读取", fi.Size())
			}
			b, err := os.ReadFile(full)
			if err != nil {
				return nil, fmt.Errorf("read file: %w", err)
			}
			if !utf8.Valid(b) {
				return nil, fmt.Errorf("%q 不是 UTF-8 文本（疑似二进制文件），不支持读取", rel)
			}
			return &readFileOut{Path: filepath.ToSlash(filepath.Clean(rel)), Size: fi.Size(), Content: string(b)}, nil
		})
}
