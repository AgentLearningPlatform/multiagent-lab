// save_file 内置工具（方案 §6.13，M11）：Agent 运行中写对话产物到项目目录。
// 路径白名单约束（LG：仅项目目录内）；写盘成功后元数据落 project_file，
// 由 runner 依据 tool.result 发 artifact.saved 事件。
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// SaveFileDeps save_file 工具的会话级依赖（装配期注入，项目会话才启用）。
type SaveFileDeps struct {
	Store          *store.Store
	ProjectID      string // 项目目录归属（必填，空则工具不可用）
	ConversationID string // 产物归属会话
	Root           string // 项目文件根目录（如 ./data/projects）
}

type saveFileIn struct {
	Name    string `json:"name" jsonschema_description:"文件名（含扩展名），不允许路径分隔符或 .."`
	Content string `json:"content" jsonschema_description:"文件文本内容"`
}

type saveFileOut struct {
	FileID string `json:"file_id" jsonschema_description:"产物文件 ID"`
	Name   string `json:"name"`
	Path   string `json:"path" jsonschema_description:"项目内相对路径"`
	Size   int64  `json:"size"`
}

var safeFileName = regexp.MustCompile(`^[\p{Han}\w][\p{Han}\w\.\- ]{0,120}$`)

// NewSaveFileTool 构造会话级 save_file 工具。
func NewSaveFileTool(deps SaveFileDeps) (einotool.BaseTool, error) {
	if deps.Store == nil || deps.ProjectID == "" || deps.Root == "" {
		return nil, fmt.Errorf("save_file: store/project/root required")
	}
	bt, err := utils.InferTool("save_file",
		"把生成的文本内容保存为项目文件（如报告、清单、代码、数据）。保存成功后文件出现在项目文件面板。",
		func(_ context.Context, in saveFileIn) (*saveFileOut, error) {
			name := strings.TrimSpace(in.Name)
			if name == "" || !safeFileName.MatchString(name) || strings.Contains(name, "..") {
				return nil, fmt.Errorf("文件名不合法: %q（仅允许中英文、数字、下划线、短横线、点、空格）", in.Name)
			}
			dir := filepath.Join(deps.Root, deps.ProjectID)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create project dir: %w", err)
			}
			rel := name
			full := filepath.Join(dir, rel)
			// 白名单核心校验：最终路径必须仍在项目目录内
			if !strings.HasPrefix(filepath.Clean(full), filepath.Clean(dir)+string(os.PathSeparator)) {
				return nil, fmt.Errorf("路径越界: %q", in.Name)
			}
			if _, err := os.Stat(full); err == nil { // 同名不覆盖：加时间戳后缀
				stamped := fmt.Sprintf("%s_%s%s", strings.TrimSuffix(name, filepath.Ext(name)),
					time.Now().Format("20060102_150405"), filepath.Ext(name))
				rel = stamped
				full = filepath.Join(dir, stamped)
			}
			if err := os.WriteFile(full, []byte(in.Content), 0o644); err != nil {
				return nil, fmt.Errorf("write file: %w", err)
			}
			fi, _ := os.Stat(full)
			pf := &store.ProjectFile{
				ProjectID: deps.ProjectID, ConversationID: deps.ConversationID,
				Name: name, Path: rel, Size: fi.Size(), Mime: mimeOf(name), Source: "artifact",
			}
			id, err := deps.Store.InsertProjectFile(pf)
			if err != nil {
				return nil, fmt.Errorf("save file metadata: %w", err)
			}
			return &saveFileOut{FileID: id, Name: name, Path: rel, Size: fi.Size()}, nil
		})
	if err != nil {
		return nil, fmt.Errorf("infer save_file tool: %w", err)
	}
	return bt, nil
}

func mimeOf(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown":
		return "text/markdown"
	case ".txt":
		return "text/plain"
	case ".json":
		return "application/json"
	case ".html":
		return "text/html"
	case ".csv":
		return "text/csv"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	default:
		return "application/octet-stream"
	}
}

// ParseSaveFileResult 从 tool.result 内容解析 save_file 输出（供 runner 发 artifact.saved）。
func ParseSaveFileResult(content string) (fileID, name, path string, ok bool) {
	var out saveFileOut
	if err := json.Unmarshal([]byte(content), &out); err != nil || out.FileID == "" {
		return "", "", "", false
	}
	return out.FileID, out.Name, out.Path, true
}
