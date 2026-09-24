// REQ-136 对话自动命名：首轮用户问题发出后，异步用默认大模型把问题提炼为对话短标题。
// 设计要点：fire-and-forget（不阻塞对话）；超时/失败回退默认名；仅当标题仍是默认
// （「新对话」开头）时覆盖，保留手动改名；提炼结果同步落审计无关表，仅更新 title。
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
)

const autoNameSchema = `{"title":"4~12 字的对话短标题（中文优先，概括用户意图，不加引号书名号）"}`

const autoNameTimeout = 15 * time.Second

// autoNameConversation POST /api/conversations/{id}/auto-name：body {input}（首轮用户输入）。
// 立即返回 {started:true}，提炼在后台 goroutine 完成；失败/超时静默保留默认名。
func (s *Server) autoNameConversation(w http.ResponseWriter, r *http.Request) {
	conv, err := s.Store.GetConversation(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Input string `json:"input"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	input := strings.TrimSpace(in.Input)
	if input == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "input 必填（首轮用户输入）"})
		return
	}
	// 手动改名保护：标题已被用户改过（非「新对话」开头）则不覆盖
	if !strings.HasPrefix(conv.Title, "新对话") {
		writeJSON(w, http.StatusOK, map[string]any{"started": false, "reason": "已手动命名"})
		return
	}
	// 后台提炼（独立 ctx，不随请求取消）
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), autoNameTimeout)
	go func() {
		defer cancel()
		prompt := "请为下面的用户提问提炼一个对话标题（4~12 个字，中文，概括意图，不要标点修饰）：\n" + input
		res, err := chat.GenerateStructured(ctx, s.Store, s.Box, "", prompt, autoNameSchema)
		if err != nil {
			return // 失败回退默认名（静默）
		}
		var out struct {
			Title string `json:"title"`
		}
		if err := json.Unmarshal(res.DraftJSON, &out); err != nil {
			return
		}
		title := strings.TrimSpace(strings.Trim(out.Title, "\"「」『』《》"))
		if title == "" || len([]rune(title)) > 24 {
			return
		}
		// 二次校验：等待期间用户可能已手动改名
		if cur, err := s.Store.GetConversation(conv.ID); err == nil && !strings.HasPrefix(cur.Title, "新对话") {
			return
		}
		_ = s.Store.SetConversationTitle(conv.ID, title)
	}()
	writeJSON(w, http.StatusOK, map[string]any{"started": true})
}
