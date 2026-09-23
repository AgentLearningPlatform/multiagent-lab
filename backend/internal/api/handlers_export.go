// REQ-113 数据导出与生命周期管理：①对话导出 Markdown（消息 + 可选过程事件附录）；
// ②设置「数据与安全」数据量概览（各表行数 + DB 文件大小 + 对话/项目级联规模）。
package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// exportConversation GET /api/conversations/{id}/export?events=1：
// 对话导出为 Markdown——头部元信息 + 消息正文（按角色分节）；events=1 时追加过程事件附录（≤500 条）。
func (s *Server) exportConversation(w http.ResponseWriter, r *http.Request) {
	conv, err := s.Store.GetConversation(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	msgs, err := s.Store.ListMessages(conv.ID)
	if err != nil {
		writeErr(w, err)
		return
	}

	owner := conv.Scope
	if conv.Scope == "agent" && conv.AgentID != nil {
		if a, err := s.Store.GetAgent(*conv.AgentID); err == nil {
			owner = "智能体 · " + a.Name
		}
	} else if conv.Scope == "project" && conv.ProjectID != nil {
		if p, err := s.Store.GetProject(*conv.ProjectID); err == nil {
			owner = "项目 · " + p.Name
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", conv.Title)
	fmt.Fprintf(&b, "- 会话 ID：%s\n", conv.ID)
	fmt.Fprintf(&b, "- 归属：%s\n", owner)
	fmt.Fprintf(&b, "- 创建时间：%s\n", conv.CreatedAt)
	fmt.Fprintf(&b, "- 导出时间：%s\n\n---\n\n", time.Now().Format("2006-01-02 15:04:05"))
	for _, m := range msgs {
		label := map[string]string{"user": "用户", "assistant": "助手", "system": "系统", "tool": "工具"}[m.Role]
		if label == "" {
			label = m.Role
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", label, strings.TrimRight(m.Content, "\n"))
	}
	if r.URL.Query().Get("events") == "1" {
		if evs, err := s.Store.ListEvents(conv.ID); err == nil && len(evs) > 0 {
			const maxEvents = 500
			if len(evs) > maxEvents {
				evs = evs[:maxEvents]
			}
			b.WriteString("---\n\n## 附录：过程事件\n\n")
			for _, ev := range evs {
				data := strings.ReplaceAll(strings.ReplaceAll(ev.Data, "\n", " "), "\r", " ")
				fmt.Fprintf(&b, "- `%s` **%s** %s\n", ev.CreatedAt, ev.Type, data)
			}
			b.WriteString("\n")
		}
	}

	name := filepath.Base(strings.ReplaceAll(strings.TrimSpace(conv.Title), "/", "_"))
	if name == "" || name == "." {
		name = "conversation"
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s-%s.md\"", name, conv.ID[:8]))
	_, _ = w.Write([]byte(b.String()))
}

// storageOverview GET /api/stats/storage：数据量概览（各表行数 + DB 文件大小 + 对话/项目级联规模）。
func (s *Server) storageOverview(w http.ResponseWriter, r *http.Request) {
	st, err := s.Store.StorageStats()
	if err != nil {
		writeErr(w, err)
		return
	}
	msgCounts, err := s.Store.ConversationMessageCounts()
	if err != nil {
		writeErr(w, err)
		return
	}
	convCounts, err := s.Store.ProjectConversationCounts()
	if err != nil {
		writeErr(w, err)
		return
	}
	type convRow struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Scope     string `json:"scope"`
		Messages  int    `json:"messages"`
		UpdatedAt string `json:"updated_at"`
	}
	convs, err := s.Store.ListConversations(store.ConversationFilter{})
	if err != nil {
		writeErr(w, err)
		return
	}
	convRows := make([]convRow, 0, len(convs))
	for _, c := range convs {
		convRows = append(convRows, convRow{ID: c.ID, Title: c.Title, Scope: c.Scope, Messages: msgCounts[c.ID], UpdatedAt: c.UpdatedAt})
	}
	type projRow struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Conversations int    `json:"conversations"`
	}
	projects, err := s.Store.ListProjects()
	if err != nil {
		writeErr(w, err)
		return
	}
	projRows := make([]projRow, 0, len(projects))
	for _, p := range projects {
		projRows = append(projRows, projRow{ID: p.ID, Name: p.Name, Conversations: convCounts[p.ID]})
	}

	var dbBytes int64
	if s.DBPath != "" {
		if fi, err := os.Stat(s.DBPath); err == nil {
			dbBytes = fi.Size()
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"db_bytes":      dbBytes,
		"stats":         st,
		"conversations": convRows,
		"projects":      projRows,
	})
}
