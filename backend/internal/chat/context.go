package chat

// REQ-201/M37 上下文工程补零（38 号路线建议 A 阶段）：
//   A1 tool 轮次保全——历史重建恢复 assistant 的 ToolCalls 与工具结果消息（run_event 派生投影，零 schema 变更；
//      无结果的调用补「结果未知」合成文本，协议配对完整，不臆断重试）；
//   A2 历史 token 预算与裁剪——按 agent.context_mode 三档（紧凑/标准/完整不限量），超限「首问+近端保留」，
//      截断必须以 run.warning 诚实标注，绝不静默丢弃；
//   A3 压缩（Compaction）——超限时对被裁前段做 LLM 摘要并持久化到 conversation.context_state
//      （消息只追加，前缀状态长期有效，后续运行直接复用不重摘；已有状态下不再叠加压缩，退化为纯裁剪）；
//   A4 上下文准入 Provider 链——见 runner.go recallChain；
//   A5 工具结果剪枝——超长工具结果按首尾确定性剪枝（无模型），保留定位提示。
// 设计依据：platform-knowledge/02_智能体/38_智能体演进路线建议_HarnessLoopGraph.md §2-A 与 36 号差距清单。

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// 上下文预算档位（token 粗估；full = 不限量 = 存量全量重放行为，诚实保留逃生口）。
const (
	ContextModeCompact  = "compact"
	ContextModeStandard = "standard"
	ContextModeFull     = "full"

	budgetCompactTokens  = 6000
	budgetStandardTokens = 24000

	// A5 工具结果剪枝阈值（字符）：超限保留首尾 + 截断标注。
	toolResultPruneChars = 4000
	toolResultHeadChars  = 2400
	toolResultTailChars  = 800

	// 压缩决策：被裁前段低于该 token 数时不值得一次 LLM 摘要，直接裁剪。
	compactionMinTokens = 1500
)

// ContextBudget 返回档位对应的 token 预算（0 = 不限量）。空值归一标准档。
func ContextBudget(mode string) int {
	switch mode {
	case ContextModeCompact:
		return budgetCompactTokens
	case ContextModeFull:
		return 0
	default: // "" 与 standard
		return budgetStandardTokens
	}
}

// EstimateTokens 粗估 token 数：ASCII 约 4 字符/token，CJK 约 2 字符/token（启发式，与业界同口径）。
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	ascii, cjk := 0, 0
	for _, r := range s {
		if r > 0x2E80 { // CJK 及全角区
			cjk++
		} else {
			ascii++
		}
	}
	return (ascii + cjk*2) / 4
}

func estimateMessages(msgs []*schema.Message) int {
	n := 0
	for _, m := range msgs {
		n += EstimateTokens(m.Content)
		for _, tc := range m.ToolCalls {
			n += EstimateTokens(tc.Function.Name) + EstimateTokens(tc.Function.Arguments)
		}
	}
	return n
}

// unknownResultText 无结果调用的合成文本（dsh TOOL_OUTCOME_UNKNOWN 纪律：告知结果未知与重试边界，不臆断）。
const unknownResultText = "（结果未知：该运行中断，未能记录此工具的执行结果。若仍需要请仅重试只读/幂等操作；可能有副作用的操作请先核实外部状态，勿盲目重试。）"

// mergeToolTurns REQ-201 A1：消息与工具事件按时间归并重建完整历史——
// assistant 消息回填 ToolCalls，其后跟配对的 tool 结果消息；中断/失败残留的调用挂到合成 assistant 上。
// 同时做 A5 工具结果剪枝。返回与输出消息对齐的来源消息 ID（合成消息为空串，供压缩状态定位）。
func mergeToolTurns(msgs []*store.Message, evs []*store.RunEvent) (out []*schema.Message, srcIDs []string) {
	type item struct {
		ts, id string
		msg    *store.Message
		ev     *store.RunEvent
	}
	timeline := make([]item, 0, len(msgs)+len(evs))
	for _, m := range msgs {
		timeline = append(timeline, item{ts: m.CreatedAt, id: m.ID, msg: m})
	}
	for _, e := range evs {
		timeline = append(timeline, item{ts: e.CreatedAt, id: e.ID, ev: e})
	}
	sort.Slice(timeline, func(i, j int) bool {
		if timeline[i].ts != timeline[j].ts {
			return timeline[i].ts < timeline[j].ts
		}
		return timeline[i].id < timeline[j].id
	})

	var pending []toolTurn
	flush := func() []*schema.Message {
		if len(pending) == 0 {
			return nil
		}
		calls := make([]schema.ToolCall, 0, len(pending))
		tools := make([]*schema.Message, 0, len(pending))
		for _, t := range pending {
			calls = append(calls, schema.ToolCall{
				ID: t.id, Function: schema.FunctionCall{Name: t.name, Arguments: t.args},
			})
			content := t.result
			if !t.hasResult {
				content = unknownResultText
			}
			tm := schema.ToolMessage(pruneToolResult(content), t.id)
			tm.ToolName = t.name
			tools = append(tools, tm)
		}
		res := append([]*schema.Message{schema.AssistantMessage("", calls)}, tools...)
		pending = nil
		return res
	}
	emitSynthetic := func() {
		if extra := flush(); len(extra) > 0 {
			out = append(out, extra...)
			srcIDs = append(srcIDs, make([]string, len(extra))...)
		}
	}

	// attachToAssistant：assistant 消息持久化于本 run 的工具事件之后，pending 即它发起的调用——
	// 回填 ToolCalls 并紧跟配对的 tool 结果消息（A1 轨迹保全）。
	attachToAssistant := func(m *store.Message) {
		calls := make([]schema.ToolCall, 0, len(pending))
		var tools []*schema.Message
		for _, t := range pending {
			calls = append(calls, schema.ToolCall{
				ID: t.id, Function: schema.FunctionCall{Name: t.name, Arguments: t.args},
			})
			content := t.result
			if !t.hasResult {
				content = unknownResultText
			}
			tm := schema.ToolMessage(pruneToolResult(content), t.id)
			tm.ToolName = t.name
			tools = append(tools, tm)
		}
		pending = nil
		out = append(out, schema.AssistantMessage(m.Content, calls))
		srcIDs = append(srcIDs, m.ID)
		if len(tools) > 0 {
			out = append(out, tools...)
			srcIDs = append(srcIDs, make([]string, len(tools))...)
		}
	}

	for _, it := range timeline {
		switch {
		case it.msg != nil && it.msg.Role == "user":
			// 孤儿调用（上一运行中断未落 assistant 消息）→ 合成 assistant 保持协议配对
			emitSynthetic()
			out = append(out, schema.UserMessage(it.msg.Content))
			srcIDs = append(srcIDs, it.msg.ID)
		case it.msg != nil && it.msg.Role == "assistant":
			attachToAssistant(it.msg)
		case it.msg != nil: // system 等其他角色原样保留
			out = append(out, schema.SystemMessage(it.msg.Content))
			srcIDs = append(srcIDs, it.msg.ID)
		case it.ev != nil && it.ev.Type == "tool.call":
			var d struct {
				ToolCallID string `json:"tool_call_id"`
				ToolName   string `json:"tool_name"`
				Arguments  string `json:"arguments"`
			}
			_ = json.Unmarshal([]byte(it.ev.Data), &d)
			pending = append(pending, toolTurn{id: d.ToolCallID, name: d.ToolName, args: d.Arguments})
		case it.ev != nil && it.ev.Type == "tool.result":
			var d struct {
				ToolCallID string `json:"tool_call_id"`
				Content    string `json:"content"`
			}
			_ = json.Unmarshal([]byte(it.ev.Data), &d)
			for i := range pending {
				if pending[i].id == d.ToolCallID {
					pending[i].result = d.Content
					pending[i].hasResult = true
					break
				}
			}
		}
	}
	emitSynthetic()
	return out, srcIDs
}

// toolTurn 时间线上的待配对工具调用。
type toolTurn struct {
	id, name, args string
	result         string
	hasResult      bool
}

// pruneToolResult A5：超长工具结果按首尾确定性剪枝（无模型），中间以截断标注替代并附取回提示。
func pruneToolResult(content string) string {
	r := []rune(content)
	if len(r) <= toolResultPruneChars {
		return content
	}
	head := string(r[:toolResultHeadChars])
	tail := string(r[len(r)-toolResultTailChars:])
	return fmt.Sprintf("%s\n\n…[工具结果过长，已剪枝 %d 字符；以上为首段、以下为末段。如需完整内容请重新调用该工具并缩小查询范围]…\n\n%s",
		head, len(r)-toolResultHeadChars-toolResultTailChars, tail)
}

// contextState 压缩持久化状态（conversation.context_state）。
type contextState struct {
	Summary          string `json:"summary"`
	ThroughMessageID string `json:"through_message_id"`
	CompactedAt      string `json:"compacted_at"`
}

// contextResult 构建产物：最终消息序列 + 需要透出的诚实标注（run.warning 由调用方发送）。
type contextResult struct {
	Messages []*schema.Message
	Warnings []string
}

// buildRunContext REQ-201 主入口：tool 轮次保全（A1）→ 压缩状态应用/生成（A3）→ 预算裁剪（A2）。
// budget=0（full 档）时仅做 A1/A5 增强，不裁剪不压缩（存量行为）。
func (s *Service) buildRunContext(ctx context.Context, conv *store.Conversation, agent *store.Agent) (contextResult, error) {
	msgs, err := s.Store.ListMessages(conv.ID)
	if err != nil {
		return contextResult{}, err
	}
	evs, err := s.Store.ListToolEvents(conv.ID)
	if err != nil {
		return contextResult{}, err
	}
	hist, srcIDs := mergeToolTurns(msgs, evs)

	mode := ""
	if agent != nil {
		mode = agent.ContextMode
	}
	budget := ContextBudget(mode)
	res := contextResult{Messages: hist}
	if budget == 0 || estimateMessages(hist) <= budget {
		return res, nil
	}

	// 1) 应用已持久化的压缩状态（前缀摘要；消息只追加，状态长期有效）。
	//    curSrc 与 res.Messages 保持对齐（摘要位为空串）。
	curSrc := srcIDs
	stateApplied := false
	if conv.ContextState != "" {
		var st contextState
		if json.Unmarshal([]byte(conv.ContextState), &st) == nil && st.Summary != "" {
			for i, id := range srcIDs {
				if id != "" && id == st.ThroughMessageID {
					summary := schema.SystemMessage(
						"以下是对本对话早期内容（截至此前）的压缩摘要，原文已归档：\n\n" + st.Summary)
					res.Messages = append([]*schema.Message{summary}, hist[i+1:]...)
					curSrc = append([]string{""}, srcIDs[i+1:]...)
					stateApplied = true
					break
				}
			}
		}
	}
	if estimateMessages(res.Messages) <= budget {
		return res, nil
	}

	// 2) 仍超预算：压缩或裁剪。保留最近 tail（预算一半，至少含最后一条 user），前段尝试 LLM 摘要。
	tailBudget := budget / 2
	keepFrom := len(res.Messages) - 1
	acc := 0
	for i := len(res.Messages) - 1; i >= 0; i-- {
		acc += EstimateTokens(res.Messages[i].Content)
		keepFrom = i
		if acc >= tailBudget {
			break
		}
	}
	if keepFrom == 0 { // 防御兜底（tail 预算 ≤ 总预算一半，理论不可达）
		return res, nil
	}
	dropped := res.Messages[:keepFrom]
	kept := res.Messages[keepFrom:]
	droppedTokens := estimateMessages(dropped)

	// 首条 user（任务起点）始终保留
	if len(dropped) > 0 && dropped[0].Role == schema.User {
		kept = append([]*schema.Message{dropped[0]}, kept...)
		dropped = dropped[1:]
	}

	summarized := false
	if !stateApplied && droppedTokens >= compactionMinTokens && s.canSummarize(ctx, agent) {
		throughID := ""
		for i := keepFrom - 1; i >= 0; i-- {
			if curSrc[i] != "" {
				throughID = curSrc[i]
				break
			}
		}
		if throughID != "" {
			if summary, serr := s.summarizeContext(ctx, agent, dropped); serr == nil && summary != "" {
				st := contextState{Summary: summary, ThroughMessageID: throughID, CompactedAt: time.Now().UTC().Format(time.RFC3339)}
				if b, jerr := json.Marshal(st); jerr == nil {
					_ = s.Store.SaveContextState(conv.ID, string(b))
				}
				res.Messages = append([]*schema.Message{schema.SystemMessage(
					"以下是对本对话早期内容（截至此前）的压缩摘要，原文已归档：\n\n" + summary)}, kept...)
				res.Warnings = append(res.Warnings, fmt.Sprintf(
					"上下文压缩：已将早期 %d 条消息（约 %d tokens）压缩为摘要并持久化（后续轮次复用，不重复压缩）", len(dropped), droppedTokens))
				summarized = true
			}
		}
	}
	if !summarized {
		res.Messages = kept
		res.Warnings = append(res.Warnings, fmt.Sprintf(
			"上下文裁剪：超出预算（约 %d tokens > %d），已丢弃最早 %d 条消息（约 %d tokens）以保持本次运行稳定；完整历史仍在对话记录中",
			estimateMessages(hist), budget, len(dropped), droppedTokens))
	}
	return res, nil
}

// canSummarize 压缩摘要前提：agent 可解析出模型实例（失败降级纯裁剪，不阻断运行）。
func (s *Service) canSummarize(ctx context.Context, agent *store.Agent) bool {
	if s.Assembler == nil || agent == nil {
		return false
	}
	cm, _, _, err := s.Assembler.buildModel(ctx, agent)
	return err == nil && cm != nil
}

// summarizeContext 复用该 agent 的模型对被裁前段生成压缩摘要（固定八段式约束输出，参照 dsh compaction 纪律）。
func (s *Service) summarizeContext(ctx context.Context, agent *store.Agent, dropped []*schema.Message) (string, error) {
	cm, _, _, err := s.Assembler.buildModel(ctx, agent)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, m := range dropped {
		switch m.Role {
		case schema.User:
			b.WriteString("[用户] " + m.Content + "\n")
		case schema.Assistant:
			b.WriteString("[助手] " + m.Content + "\n")
			for _, tc := range m.ToolCalls {
				b.WriteString("  [调用工具] " + tc.Function.Name + "(" + tc.Function.Arguments + ")\n")
			}
		case schema.Tool:
			b.WriteString("  [工具结果] " + pruneToolResult(m.Content) + "\n")
		}
	}
	sys := "你是对话历史压缩器。把给定对话前缀压缩为一份供后续轮次参考的摘要，固定输出以下八段 Markdown（无内容写「无」）：## Primary Request and Intent / ## Key Technical Concepts / ## Files and Code / ## Errors and Fixes / ## Pending Jobs / ## Current Work / ## Next Step / ## Critical Context。要求：保留精确的文件路径、命令、错误串、标识符、数值与函数签名；必须记录失败的尝试及原因；不得提及本次压缩行为本身。"
	user := "请压缩以下对话前缀：\n\n" + b.String()
	resp, err := cm.Generate(ctx, []*schema.Message{schema.SystemMessage(sys), schema.UserMessage(user)})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Content), nil
}

// trimMessagesToBudget 纯裁剪（对比窗格等不触发压缩摘要的场景）：保首条 user 与预算内最近消息。
// 返回 (裁剪后, 描述)——描述空串表示未裁剪。
func trimMessagesToBudget(msgs []*schema.Message, budget int) ([]*schema.Message, string) {
	if budget == 0 || estimateMessages(msgs) <= budget {
		return msgs, ""
	}
	tailBudget := budget / 2
	keepFrom := len(msgs) - 1
	acc := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		acc += EstimateTokens(msgs[i].Content)
		keepFrom = i
		if acc >= tailBudget {
			break
		}
	}
	if keepFrom == 0 {
		return msgs, ""
	}
	kept := msgs[keepFrom:]
	dropped := msgs[:keepFrom]
	if dropped[0].Role == schema.User {
		kept = append([]*schema.Message{dropped[0]}, kept...)
		dropped = dropped[1:]
	}
	return kept, fmt.Sprintf("上下文裁剪：超出预算（约 %d tokens > %d），已丢弃最早 %d 条消息；完整历史仍在对话记录中",
		estimateMessages(msgs), budget, len(dropped))
}
