package tool

// REQ-202/M38 Harness 执行面：todo_write 原语（模型自写任务清单，进度外显的机器可读侧）。
// 存储：conversation.todo_json（全量覆写，REQ-204/M39 进度产物双轨的机器侧雏形）；
// 过程可见：模型回执即清单渲染，前端经既有 tool.call/result 事件渲染（REQ-204 再做专用卡）。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

var todoStatuses = map[string]bool{"pending": true, "in_progress": true, "done": true}

type todoItem struct {
	Title  string `json:"title"`
	Status string `json:"status"`
}

type todoWriteIn struct {
	Todos []todoItem `json:"todos" jsonschema:"required"`
}

type todoWriteOut struct {
	Error    string   `json:"error,omitempty"` // 业务错误回执（参数/落库——文本回喂模型，不炸 run）
	Count    int      `json:"count"`
	Todos    []string `json:"todos"`
	Receipt  string   `json:"receipt"`
}

// TodoDeps todo_write 依赖：会话清单落库（conversation.todo_json）。
type TodoDeps struct {
	Store          *store.Store
	ConversationID string
}

// NewTodoWriteTool 写任务清单：全量覆写当前会话的 todo 列表（pending/in_progress/done），
// 返回渲染回执供模型确认；非破坏性（清单可随时重写），状态词汇表受限。
func NewTodoWriteTool(deps TodoDeps) (einotool.BaseTool, error) {
	if deps.Store == nil {
		return nil, fmt.Errorf("todo_write 需要 Store")
	}
	return utils.InferTool("todo_write",
		"写当前会话的任务清单（全量覆写）：逐项 {title, status}，status∈pending|in_progress|done。多步任务先列清单，每完成一项就更新状态；用于向用户外显进度。",
		func(_ context.Context, in todoWriteIn) (*todoWriteOut, error) {
			if len(in.Todos) == 0 {
				return &todoWriteOut{Error: "todos 不能为空（保持至少一项或不再调用）"}, nil
			}
			if len(in.Todos) > 50 {
				return &todoWriteOut{Error: "todo 数量上限 50"}, nil
			}
			out := make([]string, 0, len(in.Todos))
			clean := make([]todoItem, 0, len(in.Todos))
			for i, it := range in.Todos {
				title := strings.TrimSpace(it.Title)
				if title == "" {
					return &todoWriteOut{Error: fmt.Sprintf("第 %d 项 title 为空", i+1)}, nil
				}
				status := strings.TrimSpace(it.Status)
				if status == "" {
					status = "pending"
				}
				if !todoStatuses[status] {
					return &todoWriteOut{Error: fmt.Sprintf("第 %d 项 status 非法: %q（仅 pending|in_progress|done）", i+1, it.Status)}, nil
				}
				clean = append(clean, todoItem{Title: title, Status: status})
				mark := "[ ]"
				switch status {
				case "in_progress":
					mark = "[~]"
				case "done":
					mark = "[x]"
				}
				out = append(out, fmt.Sprintf("%s %s", mark, title))
			}
			b, err := json.Marshal(clean)
			if err != nil {
				return &todoWriteOut{Error: err.Error()}, nil
			}
			if err := deps.Store.SaveConversationTodo(deps.ConversationID, string(b)); err != nil {
				return &todoWriteOut{Error: err.Error()}, nil
			}
			return &todoWriteOut{Count: len(clean), Todos: out, Receipt: strings.Join(out, "\n")}, nil
		})
}
