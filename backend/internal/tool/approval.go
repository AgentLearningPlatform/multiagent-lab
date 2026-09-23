// approvalTool 工具调用人工审批包装（REQ-14 恢复语义② / LG-8 危险操作审批，M11 收尾）。
// Agent 配置 tool_approval="all" 时由装配层包裹全部工具（成员智能体 AgentTool 除外）：
// 首次调用经 compose.StatefulInterrupt 挂起等待用户批准；恢复重入时按定向恢复数据
// 执行（approve）或拒绝（返回明确拒答载荷，模型可据此调整方案）。
// 复用 REQ-14 恢复机制（checkpoint + InterruptCtx.ID 定向恢复），与 ask_human 同一管线。
package tool

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/compose"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// gob 注册：checkpoint 携带 Info/State（any），跨编解码需具名注册。
func init() {
	schema.RegisterName[*ApprovalInfo]("eino_multiagent_lab_tool_approval_info")
	schema.RegisterName[*ApprovalState]("eino_multiagent_lab_tool_approval_state")
}

// ApprovalInfo 审批请求的用户可读信息（随 run.interrupted 事件外显）。
type ApprovalInfo struct {
	ToolName  string `json:"tool_name"`
	Arguments string `json:"arguments"`
}

// ApprovalState 审批挂起状态（checkpoint gob；恢复重入时取回）。
type ApprovalState struct {
	ToolName  string `json:"tool_name"`
	Arguments string `json:"arguments"`
}

// ApprovalResumeData 恢复数据约定值：批准 / 拒绝。
const (
	ApprovalApprove = "approve"
	ApprovalDeny    = "deny"
)

type approvalTool struct {
	inner einotool.InvokableTool
	name  string
}

// NewApprovalTool 以审批包装包裹既有工具实例（inner 需为 InvokableTool）。
func NewApprovalTool(inner einotool.BaseTool, name string) (einotool.BaseTool, bool) {
	it, ok := inner.(einotool.InvokableTool)
	if !ok {
		return inner, false
	}
	return &approvalTool{inner: it, name: name}, true
}

func (t *approvalTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.inner.Info(ctx)
}

func (t *approvalTool) InvokableRun(ctx context.Context, arguments string, _ ...einotool.Option) (string, error) {
	// 恢复重入路径：本工具地址处于中断恢复流程
	if was, hasState, st := compose.GetInterruptState[*ApprovalState](ctx); was {
		if isTarget, hasData, dec := compose.GetResumeContext[string](ctx); isTarget && hasData {
			if strings.TrimSpace(dec) == ApprovalApprove {
				return t.inner.InvokableRun(ctx, arguments)
			}
			return fmt.Sprintf(`{"approved":false,"tool_name":%q,"message":"用户拒绝了本次工具调用；请勿重复调用，请询问用户希望如何调整方案。"}`, t.name), nil
		}
		if hasState && st != nil {
			// 未被定向恢复（如恢复的是别的中断点）→ 原样再中断，保持状态等待
			return "", compose.StatefulInterrupt(ctx, &ApprovalInfo{ToolName: st.ToolName, Arguments: st.Arguments}, st)
		}
	}
	// 首次调用：挂起等待人工审批
	return "", compose.StatefulInterrupt(ctx,
		&ApprovalInfo{ToolName: t.name, Arguments: arguments},
		&ApprovalState{ToolName: t.name, Arguments: arguments})
}
