// ask_human 内置工具（M11 收尾 · 中断恢复）：Agent 需要用户补充信息 / 确认决策时调用。
// 通过 eino ADK 中断机制（compose.StatefulInterrupt）挂起当前运行并保存 checkpoint，
// 用户答复后由平台经 Runner.ResumeWithParams 按 InterruptCtx.ID 定向恢复——
// 工具被重入时经 GetInterruptState / GetResumeContext 取回状态与答复（显式定向恢复契约）。
package tool

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/compose"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

// gob 注册：checkpoint（gob 编码）会携带 Info/State（any），跨编解码需具名注册。
func init() {
	schema.RegisterName[*AskHumanInfo]("eino_multiagent_lab_tool_ask_human_info")
	schema.RegisterName[*AskHumanState]("eino_multiagent_lab_tool_ask_human_state")
}

// AskHumanInfo 中断的用户可读信息（不持久化，随中断事件外显）。
type AskHumanInfo struct {
	Question string   `json:"question"`
	Choices  []string `json:"choices,omitempty"`
}

// AskHumanState 中断持久化状态（checkpoint gob；恢复重入时取回）。
type AskHumanState struct {
	Question string   `json:"question"`
	Choices  []string `json:"choices"`
}

type askHumanIn struct {
	Question string   `json:"question" jsonschema_description:"要向用户提出的问题（具体、可直接回答）"`
	Choices  []string `json:"choices,omitempty" jsonschema_description:"可选的候选项（可选，供用户快速选择）"`
}

type askHumanOut struct {
	Answer string `json:"answer" jsonschema_description:"用户的答复"`
}

// NewAskHumanTool 构造 ask_human 工具实例（无会话级依赖；interrupt 上下文按次携带）。
func NewAskHumanTool() (einotool.BaseTool, error) {
	return utils.InferTool("ask_human",
		"当继续任务需要用户补充信息、确认方案或做出选择时调用。调用会中断当前运行并向用户展示问题；用户答复后自动恢复执行，本工具返回用户的答复。",
		askHumanFn)
}

func askHumanFn(ctx context.Context, in askHumanIn) (*askHumanOut, error) {
	// 恢复重入路径：本工具地址处于中断恢复流程
	if was, hasState, st := compose.GetInterruptState[*AskHumanState](ctx); was {
		if hasData, answer := resumeAnswer(ctx); hasData {
			return &askHumanOut{Answer: answer}, nil
		}
		if hasState && st != nil {
			// 未被定向恢复（如恢复的是别的中断点）→ 原样再中断，保持状态等待
			return nil, compose.StatefulInterrupt(ctx, &AskHumanInfo{Question: st.Question, Choices: st.Choices}, st)
		}
	}
	q := strings.TrimSpace(in.Question)
	if q == "" {
		return nil, fmt.Errorf("question 必填")
	}
	st := &AskHumanState{Question: q, Choices: in.Choices}
	return nil, compose.StatefulInterrupt(ctx, &AskHumanInfo{Question: q, Choices: in.Choices}, st)
}

// resumeAnswer 读取定向恢复携带的答复数据（非 string 形态视为无数据）。
func resumeAnswer(ctx context.Context) (bool, string) {
	isTarget, hasData, data := compose.GetResumeContext[string](ctx)
	if !isTarget || !hasData {
		return false, ""
	}
	return true, strings.TrimSpace(data)
}
