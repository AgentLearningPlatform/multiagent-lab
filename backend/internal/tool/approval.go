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
	"time"

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
	// REQ-231③：挂起时刻（挂起超时判定用；零值=旧挂起不回溯）
	SuspendedAt string `json:"suspended_at,omitempty"`
}

// ApprovalResumeData 恢复数据约定值：批准 / 拒绝。
const (
	ApprovalApprove = "approve"
	ApprovalDeny    = "deny"
)

// DangerousBuiltinTools REQ-231① 危险工具清单（danger 档审批范围——写类内置 + 出网取数；
// read-only（grep/glob/read_file/current_time/ask_human/list_files）免审。口径见 51 号 W2）。
var DangerousBuiltinTools = map[string]bool{
	"write_file": true,
	"save_file":  true,
	"todo_write": true,
	"http_fetch": true,
}

// IsDangerousTool REQ-231①：danger 档判定——内置写类 + 连接器/mcp 非 read-only 前缀工具
// （{连接器实例名}__{tool} 形态一律视为危险：装配侧已对 read_only 连接器摘除写工具，
// 但 mcp 直通无法静态判定，保守全审；SSH/k8s 同口径）。ontology__ onto_* 与内置只读免审。
func IsDangerousTool(name string) bool {
	if DangerousBuiltinTools[name] {
		return true
	}
	// 连接器/MCP 前缀工具（oo onto_* 只读除外）
	if i := strings.Index(name, "__"); i > 0 {
		prefix := name[:i]
		if prefix == "ontology" || prefix == "oo" {
			return false
		}
		return true
	}
	return false
}

type approvalTool struct {
	inner einotool.InvokableTool
	name  string
	// REQ-231②：审批豁免清单（个工具覆盖档位——清单内工具直接放行不挂起）
	exempt map[string]bool
	// REQ-231③：挂起超时（小时；0=不限）——恢复重入时超时自动 deny 并附告警载荷
	timeoutHours float64
}

// ApprovalPolicy REQ-231 审批策略（装配层注入）。
type ApprovalPolicy struct {
	ExemptTools  []string // 豁免清单（tool_approval=danger/all 下清单内工具免审）
	TimeoutHours float64  // 挂起超时（0=不限）
}

// NewApprovalTool 以审批包装包裹既有工具实例（inner 需为 InvokableTool；policy 为空取零值=旧行为）。
func NewApprovalTool(inner einotool.BaseTool, name string, policy *ApprovalPolicy) (einotool.BaseTool, bool) {
	it, ok := inner.(einotool.InvokableTool)
	if !ok {
		return inner, false
	}
	t := &approvalTool{inner: it, name: name}
	if policy != nil {
		if len(policy.ExemptTools) > 0 {
			t.exempt = map[string]bool{}
			for _, n := range policy.ExemptTools {
				t.exempt[n] = true
			}
		}
		t.timeoutHours = policy.TimeoutHours
	}
	return t, true
}

func (t *approvalTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.inner.Info(ctx)
}

func (t *approvalTool) InvokableRun(ctx context.Context, arguments string, _ ...einotool.Option) (string, error) {
	// REQ-231②：豁免清单内工具直接放行（恢复重入的旧挂起不受影响——豁免只作用于新挂起）
	if t.exempt != nil && t.exempt[t.name] {
		return t.inner.InvokableRun(ctx, arguments)
	}
	// 恢复重入路径：本工具地址处于中断恢复流程
	if was, hasState, st := compose.GetInterruptState[*ApprovalState](ctx); was {
		if isTarget, hasData, dec := compose.GetResumeContext[string](ctx); isTarget && hasData {
			if strings.TrimSpace(dec) == ApprovalApprove {
				// REQ-224：approval.granted 结构化审计事件（决策上下文=恢复定向批准）
				EmitEvent(ctx, "approval.granted", map[string]any{
					"tool_name": t.name, "args_digest": argsDigest(arguments),
					"decision": "approve", "decision_source": "manual",
				})
				return t.inner.InvokableRun(ctx, arguments)
			}
			EmitEvent(ctx, "approval.denied", map[string]any{
				"tool_name": t.name, "args_digest": argsDigest(arguments),
				"decision": "deny", "decision_source": "manual",
			})
			return fmt.Sprintf(`{"approved":false,"tool_name":%q,"message":"用户拒绝了本次工具调用；请勿重复调用，请询问用户希望如何调整方案。"}`, t.name), nil
		}
		if hasState && st != nil {
			// REQ-231③：挂起超时——恢复重入且挂起时刻超过 timeout → 自动 deny（附超时语义，
			// 模型可据此调整方案；零值/不可解析=不限，旧挂起不回溯）
			if t.timeoutHours > 0 && st.SuspendedAt != "" {
				if at, perr := time.Parse(time.RFC3339, st.SuspendedAt); perr == nil && time.Since(at) > time.Duration(t.timeoutHours*float64(time.Hour)) {
					EmitEvent(ctx, "approval.denied", map[string]any{
						"tool_name": t.name, "args_digest": argsDigest(arguments),
						"decision": "deny", "decision_source": "timeout",
						"timeout_hours": t.timeoutHours,
					})
					return fmt.Sprintf(`{"approved":false,"timeout":true,"tool_name":%q,"message":"审批挂起超过 %.1f 小时未被处理，已自动拒绝本次调用；请勿重复调用，请询问用户希望如何调整方案。"}`, t.name, t.timeoutHours), nil
				}
			}
			// 未被定向恢复（如恢复的是别的中断点）→ 原样再中断，保持状态等待
			return "", compose.StatefulInterrupt(ctx, &ApprovalInfo{ToolName: st.ToolName, Arguments: st.Arguments}, st)
		}
	}
	// 首次调用：挂起等待人工审批（REQ-231③ 挂起时刻随 checkpoint 记录）
	return "", compose.StatefulInterrupt(ctx,
		&ApprovalInfo{ToolName: t.name, Arguments: arguments},
		&ApprovalState{ToolName: t.name, Arguments: arguments, SuspendedAt: time.Now().UTC().Format(time.RFC3339)})
}
