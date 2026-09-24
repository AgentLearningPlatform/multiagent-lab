// REQ-131/M18：Agent 对外 MCP 服务化（§6.13）——/mcp Streamable HTTP 端点。
// 每个开启「对外服务」的 Agent → 一个 MCP 工具 agent_{id}（可配 tool_name 覆盖）；
// 调用 = 一次独立 run（工具式先行，无状态），最终回复作为工具响应返回。
// 安全边界：Agent 级 Bearer Token（list/call 均需有效 token，call 需精确匹配该 Agent）；
// tool_approval≠off 的 Agent 在 server 模式默认拒绝（无 UI 交互面）；装配层拦截本平台 /mcp 自引用。
package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

type mcpBearerKey struct{}

// RandToken 生成 64 位十六进制随机 Token（Agent 级 Bearer）。
func RandToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// mcpHandler 构建并缓存 /mcp 的 Streamable HTTP handler。
// 工具表在构建时注册（enabled Agent 快照）；Agent 增删/开关经 invalidateMCP() 失效重建。
func (s *Server) mcpHandler() http.Handler {
	s.mcpMu.Lock()
	defer s.mcpMu.Unlock()
	if s.mcpHTTP != nil {
		return s.mcpHTTP
	}
	mcpSrv := mcpserver.NewMCPServer("eino-multiagent-lab", "0.1.0")
	agents, err := s.Store.ListAgents()
	if err != nil {
		log.Printf("[mcp] list agents: %v", err)
	}
	seen := map[string]bool{}
	for _, a := range agents {
		if !a.McpServe.Enabled {
			continue
		}
		name := mcpToolName(*a)
		if seen[name] {
			log.Printf("[mcp] 工具名冲突 %q，跳过（agent %s）", name, a.ID)
			continue
		}
		seen[name] = true
		s.addAgentTool(mcpSrv, a.ID, name)
	}
	s.mcpHTTP = mcpserver.NewStreamableHTTPServer(mcpSrv)
	return s.mcpHTTP
}

// invalidateMCP 使 /mcp 工具表缓存失效（Agent mcp_serve 变更后调用）。
func (s *Server) invalidateMCP() {
	s.mcpMu.Lock()
	s.mcpHTTP = nil
	s.mcpMu.Unlock()
}

func mcpToolName(a store.Agent) string {
	if a.McpServe.ToolName != "" {
		return a.McpServe.ToolName
	}
	return "agent_" + a.ID
}

// addAgentTool 注册 agent_{id} 工具。handler 内每次调用实时回读 Agent（enabled/token/审批取最新态）。
func (s *Server) addAgentTool(mcpSrv *mcpserver.MCPServer, agentID, name string) {
	desc := "调用一个智能体并返回其最终回复。"
	if a, err := s.Store.GetAgent(agentID); err == nil && a.Description != "" {
		desc = a.Description + "\n" + desc
	}
	tool := mcp.NewTool(name,
		mcp.WithDescription(desc),
		mcp.WithString("input", mcp.Required(), mcp.Description("发给智能体的任务或问题")),
		mcp.WithString("session_id", mcp.Description("预留：会话式多轮（当前工具式无状态，忽略）")),
	)
	mcpSrv.AddTool(tool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		a, err := s.Store.GetAgent(agentID)
		if err != nil || a == nil || !a.McpServe.Enabled {
			return mcp.NewToolResultError("该智能体不存在或已关闭对外服务"), nil
		}
		// Bearer 精确匹配（middleware 已校验 token 属于某 enabled Agent，这里再校验就是本 Agent 的）
		if tok, _ := ctx.Value(mcpBearerKey{}).(string); tok == "" || tok != a.McpServe.Token {
				return mcp.NewToolResultError("unauthorized: token 与该智能体不匹配"), nil
		}
		// 审批默认拒绝（server 模式无 UI 交互面，§6.13 安全边界）
		if a.ToolApproval != "" && a.ToolApproval != "off" {
			return mcp.NewToolResultError("该智能体启用了工具人工审批，server 模式暂无交互面，本次调用已拒绝（可在智能体配置中关闭审批后再试）"), nil
		}
		input, _ := req.GetArguments()["input"].(string)
		if strings.TrimSpace(input) == "" {
			return mcp.NewToolResultError("input 必填"), nil
		}
		toolApproval := "" // MCP server 模式无审批交互面（审批 Agent 在上方已默认拒绝）
		conv, err := s.Store.CreateConversation(&store.Conversation{
			Scope: "agent", AgentID: &a.ID, Title: "MCP 调用 · " + a.Name, ToolApproval: &toolApproval,
		})
		if err != nil {
			return mcp.NewToolResultError("create conversation: " + err.Error()), nil
		}
		// 工具式：一次独立 run；事件照常入库（对话窗口可回放），最终回复取助手消息
		res, err := s.Chat.Run(ctx, conv, a, store.NewID(), input, 0, false, func(*chat.Event) {})
		if err != nil {
			return mcp.NewToolResultError("run failed: " + err.Error()), nil
		}
		if res.Stopped {
			return mcp.NewToolResultError("运行被停止，无最终回复"), nil
		}
		msgs, err := s.Store.ListMessages(conv.ID)
		if err != nil {
			return mcp.NewToolResultError("load reply: " + err.Error()), nil
		}
		reply := ""
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == "assistant" {
				reply = msgs[i].Content
				break
			}
		}
		if reply == "" {
			return mcp.NewToolResultError("运行未产生回复"), nil
		}
		return mcp.NewToolResultText(reply), nil
	})
}

// bearerToken 解析 Authorization: Bearer xxx。
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

// mcpAuth 每请求鉴权：Bearer 必须匹配任一 enabled Agent 的 Token；
// 匹配的 token 放入 context，供工具处理器做「本 Agent 精确匹配」二次校验。
func (s *Server) mcpAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := bearerToken(r)
		if tok == "" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="eino-agents"`)
			http.Error(w, `{"error":"missing bearer token"}`, http.StatusUnauthorized)
			return
		}
		agents, err := s.Store.ListAgents()
		if err != nil {
			http.Error(w, `{"error":"internal"}`, http.StatusInternalServerError)
			return
		}
		ok := false
		for _, a := range agents {
			if a.McpServe.Enabled && a.McpServe.Token != "" && a.McpServe.Token == tok {
				ok = true
				break
			}
		}
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="eino-agents"`)
			http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), mcpBearerKey{}, tok)))
	})
}

// ---- mcp-serve 管理端点 ----

type mcpServeInfoOut struct {
	Enabled    bool   `json:"enabled"`
	ToolName   string `json:"tool_name"`
	Endpoint   string `json:"endpoint"`
	TokenMask  string `json:"token_mask,omitempty"`
	Curl       string `json:"curl,omitempty"`
	Configured bool   `json:"configured"` // 是否已有 Token（开启但未生成时前端引导重置）
}

func maskToken(tok string) string {
	if len(tok) <= 10 {
		return ""
	}
	return tok[:6] + "****" + tok[len(tok)-4:]
}

// getAgentMcpServe GET /api/agents/{id}/mcp-serve：端点/工具名/调用示例（Token 掩码）。
func (s *Server) getAgentMcpServe(w http.ResponseWriter, r *http.Request) {
	a, err := s.Store.GetAgent(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	out := mcpServeInfoOut{
		Enabled:    a.McpServe.Enabled,
		ToolName:   mcpToolName(*a),
		Endpoint:   "/mcp",
		Configured: a.McpServe.Token != "",
	}
	if a.McpServe.Token != "" {
		out.TokenMask = maskToken(a.McpServe.Token)
		host := r.Host
		out.Curl = fmt.Sprintf(`curl -X POST "http://%s/mcp" -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" -d '{"jsonrpc":"2.0","method":"tools/call","params":{"name":"%s","arguments":{"input":"你好"}},"id":1}'`, host, out.ToolName)
	}
	writeJSON(w, http.StatusOK, out)
}

// resetAgentMcpToken POST /api/agents/{id}/mcp-serve/reset：重置 Token（自动开启时生成）。
func (s *Server) resetAgentMcpToken(w http.ResponseWriter, r *http.Request) {
	a, err := s.Store.GetAgent(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	a.McpServe.Token = RandToken()
	if _, err := s.Store.UpdateAgent(a); err != nil {
		writeErr(w, err)
		return
	}
	s.invalidateMCP()
	writeJSON(w, http.StatusOK, map[string]string{"token_mask": maskToken(a.McpServe.Token)})
}

// normalizeMcpServe 保存前补齐对外服务配置：开启且无 Token → 自动生成；关闭则保留原 Token。
// 返回 true 表示发生变更（需 invalidateMCP）。
func (s *Server) normalizeMcpServe(a *store.Agent, prev store.Agent) bool {
	if a.McpServe.Enabled && a.McpServe.Token == "" {
		a.McpServe.Token = RandToken()
		return true
	}
	if a.McpServe != prev.McpServe {
		return true
	}
	return false
}

// mcpSync 由 handlers 在 Agent 增删改后调用：重建 /mcp 工具表。
func (s *Server) mcpSync() { s.invalidateMCP() }

