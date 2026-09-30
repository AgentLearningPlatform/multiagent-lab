// REQ-186 阶段二：平台助手 L0 只读工具面（分级白名单——只读全开，写类不开放）。
// 注册进通用工具注册表（builtin 来源），由内置助手行（is_builtin=1）的 tools 字段引用；
// 用户 Agent 勾选同一工具亦可用（注册表开放性，REQ-77），风险面为纯只读。
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/fsutil"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/tool"
)

// AssistantDeps L0 工具的只读依赖（main.go 装配时注入；与 api 层 docRead 同源口径）。
type AssistantDeps struct {
	Store         *store.Store
	DocsRoot      string
	ResearchRoot  string
	KnowledgeRoot string
	// DefaultPrompt REQ-213：内置提示词基座（api.AssistantDefaultPrompt 注入）——
	// get_assistant_config 以「行内 instruction == 基座」判「未微调」（assistant_config 表退役后单源读行）。
	DefaultPrompt string
}

// RegisterAssistantTools 注册 L0 只读工具：doc_read / list_model_connections / list_agents /
// list_kbs / get_assistant_config。同名覆盖幂等。
func RegisterAssistantTools(r *tool.Registry, deps AssistantDeps) error {
	entries := []*tool.Entry{
		assistantDocRead(deps),
		assistantListModels(deps),
		assistantListAgents(deps),
		assistantListKBs(deps),
		assistantGetConfig(deps),
	}
	for _, e := range entries {
		if err := r.Register(e); err != nil {
			return err
		}
	}
	return nil
}

// assistantDocRead L0：读白名单目录（docs/ + research/ + platform-knowledge/）下的 .md 全文。
// 与 api.docRead 同一白名单与防越界口径（REQ-140/150/161 语义，供助手精读引用）。
func assistantDocRead(deps AssistantDeps) *tool.Entry {
	return &tool.Entry{
		ID:          "doc_read",
		Name:        "doc_read",
		Description: "读取平台内部文档全文（docs/、research/、platform-knowledge/ 下 .md，只读）——回答平台使用问题前先精读相关文档",
		Source:      tool.SourceBuiltin,
		New: func(ctx context.Context) (einotool.BaseTool, error) {
			fn := func(ctx context.Context, in assistantDocReadIn) (string, error) {
				rel := strings.TrimSpace(in.Path)
				if rel == "" || !strings.HasSuffix(rel, ".md") {
					return "", fmt.Errorf("path 必填且仅支持 .md 文档")
				}
				clean := filepath.ToSlash(filepath.Clean(rel))
				if strings.Contains(clean, "..") {
					return "", fmt.Errorf("路径越界")
				}
				var root, sub string
				switch {
				case strings.HasPrefix(clean, "docs/"):
					root, sub = deps.DocsRoot, strings.TrimPrefix(clean, "docs/")
				case strings.HasPrefix(clean, "research/"):
					root, sub = deps.ResearchRoot, strings.TrimPrefix(clean, "research/")
				case strings.HasPrefix(clean, "platform-knowledge/"):
					root, sub = deps.KnowledgeRoot, strings.TrimPrefix(clean, "platform-knowledge/")
				default:
					return "", fmt.Errorf("仅支持 docs/、research/ 或 platform-knowledge/ 目录下的文档")
				}
				abs, err := fsutil.SafeJoin(root, sub)
				if err != nil {
					return "", fmt.Errorf("路径越界")
				}
				b, err := os.ReadFile(abs)
				if err != nil {
					return "", fmt.Errorf("文档不存在或不可读: %v", err)
				}
				const maxBytes = 48 * 1024
				content := string(b)
				if len(content) > maxBytes {
					content = content[:maxBytes] + "\n\n…（文档过长已截断）"
				}
				return content, nil
			}
			return utils.InferTool("doc_read", "读取平台内部文档（docs/、research/、platform-knowledge/ 下 .md）全文，用于精读后回答平台使用问题。入参 path 如 platform-knowledge/02_智能体/智能体模块导读.md", fn)
		},
	}
}

type assistantDocReadIn struct {
	Path string `json:"path" jsonschema:"文档相对路径（docs/ 或 research/ 或 platform-knowledge/ 前缀，.md 结尾）"`
}

// assistantListModels L0：模型连接清单（脱敏——只回名称/类型/模型名/启用与默认态，不含 Key/BaseURL）。
func assistantListModels(deps AssistantDeps) *tool.Entry {
	return &tool.Entry{
		ID: "list_model_connections", Name: "list_model_connections",
		Description: "列出平台已配置的模型连接（名称/类型/启用态，不含密钥，只读）", Source: tool.SourceBuiltin,
		New: func(ctx context.Context) (einotool.BaseTool, error) {
			fn := func(ctx context.Context, _ struct{}) (string, error) {
				conns, err := deps.Store.ListConnections()
				if err != nil {
					return "", err
				}
				var b strings.Builder
				b.WriteString(fmt.Sprintf("共 %d 条模型连接：\n", len(conns)))
				for _, c := range conns {
					state := "停用"
					if c.Enabled {
						state = "启用"
					}
					def := ""
					if c.IsDefault {
						def = " [默认]"
					}
					key := "未配Key"
					if c.HasKey {
						key = "已配Key"
					}
					fmt.Fprintf(&b, "- %s · %s · %s · %s%s\n", c.Name, c.ConnType, state, key, def)
				}
				return b.String(), nil
			}
			return utils.InferTool("list_model_connections", "列出平台已配置的模型连接（名称/类型/启用态，不含密钥）", fn)
		},
	}
}

// assistantListAgents L0：用户智能体清单（不含内置助手自身）。
func assistantListAgents(deps AssistantDeps) *tool.Entry {
	return &tool.Entry{
		ID: "list_agents", Name: "list_agents",
		Description: "列出平台中的用户智能体清单（名称与描述，只读）", Source: tool.SourceBuiltin,
		New: func(ctx context.Context) (einotool.BaseTool, error) {
			fn := func(ctx context.Context, _ struct{}) (string, error) {
				agents, err := deps.Store.ListAgents()
				if err != nil {
					return "", err
				}
				var b strings.Builder
				n := 0
				for _, a := range agents {
					if a.IsBuiltin {
						continue
					}
					n++
					fmt.Fprintf(&b, "- %s（%s）\n", a.Name, a.Description)
				}
				if n == 0 {
					return "暂无用户智能体", nil
				}
				return fmt.Sprintf("共 %d 个智能体：\n%s", n, b.String()), nil
			}
			return utils.InferTool("list_agents", "列出平台中的用户智能体清单（名称与描述）", fn)
		},
	}
}

// assistantListKBs L0：知识库清单（名称与模式）。
func assistantListKBs(deps AssistantDeps) *tool.Entry {
	return &tool.Entry{
		ID: "list_kbs", Name: "list_kbs",
		Description: "列出平台中的知识库清单（名称与模式，只读）", Source: tool.SourceBuiltin,
		New: func(ctx context.Context) (einotool.BaseTool, error) {
			fn := func(ctx context.Context, _ struct{}) (string, error) {
				kbs, err := deps.Store.ListKnowledgeBases()
				if err != nil {
					return "", err
				}
				if len(kbs) == 0 {
					return "暂无知识库", nil
				}
				var b strings.Builder
				b.WriteString(fmt.Sprintf("共 %d 个知识库：\n", len(kbs)))
				for _, k := range kbs {
					fmt.Fprintf(&b, "- %s（%s 模式）\n", k.Name, k.Mode)
				}
				return b.String(), nil
			}
			return utils.InferTool("list_kbs", "列出平台中的知识库清单（名称与模式）", fn)
		},
	}
}

// assistantGetConfig L0：平台助手自身配置（模型覆盖/温度/提示词是否微调）。
// REQ-213：读 agent 内置行单源（assistant_config 表已退役，REQ-192 遗留收口——原实现读旧表恒空值）。
func assistantGetConfig(deps AssistantDeps) *tool.Entry {
	return &tool.Entry{
		ID: "get_assistant_config", Name: "get_assistant_config",
		Description: "查询平台助手自身的配置（模型覆盖/温度/提示词是否微调，只读）", Source: tool.SourceBuiltin,
		New: func(ctx context.Context) (einotool.BaseTool, error) {
			fn := func(ctx context.Context, _ struct{}) (string, error) {
				a, err := deps.Store.GetAgent("builtin-assistant")
				if err != nil {
					return "", err
				}
				tuned := strings.TrimSpace(a.Instruction) != "" && a.Instruction != deps.DefaultPrompt
				out := map[string]any{
					"model_conn_id": derefAgentStr(a.ModelConnID),
					"temperature":   a.Temperature,
					"prompt_tuned":  tuned,
				}
				b, _ := json.MarshalIndent(out, "", "  ")
				return string(b), nil
			}
			return utils.InferTool("get_assistant_config", "查询平台助手自身的配置（模型覆盖/温度/提示词是否微调）", fn)
		},
	}
}

// derefAgentStr nil 安全解引用（chat 包本地版，避免引 api）。
func derefAgentStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
