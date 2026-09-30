package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

func toolCallEvent(runID, callID, name, args, ts string) *store.RunEvent {
	b, _ := json.Marshal(map[string]string{"tool_call_id": callID, "tool_name": name, "arguments": args})
	return &store.RunEvent{ID: callID + "-c", RunID: runID, Type: "tool.call", Data: string(b), CreatedAt: ts}
}

func toolResultEvent(runID, callID, content, ts string) *store.RunEvent {
	b, _ := json.Marshal(map[string]string{"tool_call_id": callID, "content": content})
	return &store.RunEvent{ID: callID + "-r", RunID: runID, Type: "tool.result", Data: string(b), CreatedAt: ts}
}

// A1：tool 轮次保全——assistant 消息回填 ToolCalls，tool 结果成对重建。
func TestMergeToolTurnsRestoresToolTrajectory(t *testing.T) {
	msgs := []*store.Message{
		{ID: "m1", Role: "user", Content: "现在几点", CreatedAt: "2026-09-29T10:00:00Z"},
		{ID: "m2", Role: "assistant", Content: "现在是 10 点。", CreatedAt: "2026-09-29T10:00:05Z"},
		{ID: "m3", Role: "user", Content: "谢谢", CreatedAt: "2026-09-29T10:01:00Z"},
	}
	evs := []*store.RunEvent{
		toolCallEvent("r1", "c1", "current_time", "{}", "2026-09-29T10:00:01Z"),
		toolResultEvent("r1", "c1", "2026-09-29 10:00", "2026-09-29T10:00:02Z"),
	}
	out, srcIDs := mergeToolTurns(msgs, evs)
	if len(out) != 4 {
		t.Fatalf("期望 4 条消息（user/assistant+toolcalls/tool/user），got %d", len(out))
	}
	if len(out[1].ToolCalls) != 1 || out[1].ToolCalls[0].Function.Name != "current_time" {
		t.Fatalf("assistant 未回填 ToolCalls: %+v", out[1])
	}
	if out[2].Role != schema.Tool || out[2].ToolCallID != "c1" || !strings.Contains(out[2].Content, "10:00") {
		t.Fatalf("tool 结果消息未正确重建: %+v", out[2])
	}
	if srcIDs[1] != "m2" || srcIDs[3] != "m3" {
		t.Fatalf("srcIDs 对齐错误: %v", srcIDs)
	}
}

// A1：无结果调用补「结果未知」合成文本 + 合成 assistant 保持协议配对。
func TestMergeToolTurnsSynthesizesUnknownResult(t *testing.T) {
	msgs := []*store.Message{
		{ID: "m1", Role: "user", Content: "跑个任务", CreatedAt: "2026-09-29T10:00:00Z"},
		{ID: "m2", Role: "user", Content: "还在吗", CreatedAt: "2026-09-29T10:05:00Z"},
	}
	evs := []*store.RunEvent{toolCallEvent("r1", "c1", "save_file", "{}", "2026-09-29T10:00:10Z")}
	out, _ := mergeToolTurns(msgs, evs)
	// user / 合成assistant(toolcalls) / tool(未知) / user
	if len(out) != 4 || len(out[1].ToolCalls) != 1 {
		t.Fatalf("合成配对失败: %d 条", len(out))
	}
	if !strings.Contains(out[2].Content, "结果未知") {
		t.Fatalf("无结果调用未补合成文本: %q", out[2].Content)
	}
}

// A1 边界：孤儿调用早于首条 user（历史窗口外事件）→ 丢弃，不产生前导合成 assistant。
func TestMergeToolTurnsDropsOrphanBeforeFirstUser(t *testing.T) {
	msgs := []*store.Message{
		{ID: "m1", Role: "user", Content: "你好", CreatedAt: "2026-09-30T08:00:00Z"},
		{ID: "m2", Role: "assistant", Content: "你好！", CreatedAt: "2026-09-30T08:00:05Z"},
	}
	evs := []*store.RunEvent{toolCallEvent("dead", "dg1", "save_file", "{}", "2026-09-30T00:09:30Z")}
	out, _ := mergeToolTurns(msgs, evs)
	if len(out) != 2 {
		t.Fatalf("应仅保留 user/assistant 两条（孤儿调用丢弃），got %d: %+v", len(out), out)
	}
	if out[0].Role != schema.User {
		t.Fatalf("首条必须是 user，got %s", out[0].Role)
	}
}

// A5：超长工具结果首尾剪枝并带截断标注。
func TestPruneToolResult(t *testing.T) {
	long := strings.Repeat("头", 3000) + strings.Repeat("中", 5000) + strings.Repeat("尾", 900)
	pruned := pruneToolResult(long)
	if len([]rune(pruned)) >= len([]rune(long)) {
		t.Fatal("剪枝未生效")
	}
	if !strings.Contains(pruned, "已剪枝") || !strings.HasSuffix(pruned, "尾尾尾") {
		t.Fatal("剪枝缺标注或末段")
	}
	if pruneToolResult("短结果") != "短结果" {
		t.Fatal("短结果不应剪枝")
	}
}

// A2：预算裁剪——保首条 user 与近端，超限部分丢弃。
func TestTrimMessagesToBudget(t *testing.T) {
	var msgs []*schema.Message
	msgs = append(msgs, schema.UserMessage("任务起点："+strings.Repeat("长", 200)))
	for i := 0; i < 200; i++ {
		msgs = append(msgs, schema.UserMessage(strings.Repeat("填", 200)), schema.AssistantMessage(strings.Repeat("答", 200), nil))
	}
	msgs = append(msgs, schema.UserMessage("最新问题"))
	kept, desc := trimMessagesToBudget(msgs, 2000)
	if desc == "" {
		t.Fatal("超预算应返回裁剪描述")
	}
	if kept[0].Role != schema.User || !strings.HasPrefix(kept[0].Content, "任务起点") {
		t.Fatal("首条 user 应保留")
	}
	if kept[len(kept)-1].Content != "最新问题" {
		t.Fatal("最新消息应保留")
	}
	if estimateMessages(kept) > 2000 {
		t.Fatalf("裁剪后仍超预算: %d", estimateMessages(kept))
	}
	// 不限量与未超限直通
	if _, d2 := trimMessagesToBudget(msgs, 0); d2 != "" {
		t.Fatal("full 档不应裁剪")
	}
	if _, d3 := trimMessagesToBudget(msgs[:1], 2000); d3 != "" {
		t.Fatal("未超预算不应裁剪")
	}
}

// A2/A3：buildRunContext 真库链路——超预算触发压缩/裁剪并透出诚实标注；full 档不裁剪。
func TestBuildRunContextBudgetAndCompaction(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/ctx.db")
	if err != nil {
		t.Fatal(err)
	}
	ag, err := st.CreateAgent(&store.Agent{Name: "ctx-bot", Instruction: "x"})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := st.CreateConversation(&store.Conversation{Scope: "agent", AgentID: &ag.ID})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 120; i++ {
		if _, err := st.InsertMessage(&store.Message{ConversationID: conv.ID, Role: "user", Content: strings.Repeat("问", 300)}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.InsertMessage(&store.Message{ConversationID: conv.ID, Role: "assistant", Content: strings.Repeat("答", 300)}); err != nil {
			t.Fatal(err)
		}
	}
	svc := &Service{Store: st}

	// standard 档：无法解析模型（Assembler nil）→ 降级纯裁剪，但必须透出诚实标注
	cr, err := svc.buildRunContext(context.Background(), conv, ag)
	if err != nil {
		t.Fatal(err)
	}
	if len(cr.Warnings) == 0 {
		t.Fatal("超预算应透出 run.warning 文案")
	}
	if estimateMessages(cr.Messages) > ContextBudget("") {
		t.Fatalf("裁剪后仍超预算: %d", estimateMessages(cr.Messages))
	}

	// full 档：全量重放（存量行为）
	agFull, _ := st.CreateAgent(&store.Agent{Name: "full-bot", ContextMode: ContextModeFull})
	cr2, err := svc.buildRunContext(context.Background(), conv, agFull)
	if err != nil {
		t.Fatal(err)
	}
	if len(cr2.Warnings) != 0 || estimateMessages(cr2.Messages) <= ContextBudget("") {
		t.Fatal("full 档应全量保留不告警")
	}
}

// ContextBudget 档位映射。
func TestContextBudget(t *testing.T) {
	if ContextBudget("") != budgetStandardTokens || ContextBudget(ContextModeStandard) != budgetStandardTokens {
		t.Fatal("默认应归一标准档")
	}
	if ContextBudget(ContextModeCompact) != budgetCompactTokens {
		t.Fatal("compact 档错误")
	}
	if ContextBudget(ContextModeFull) != 0 {
		t.Fatal("full 档应不限量")
	}
}

// EstimateTokens：CJK 计价高于 ASCII。
func TestEstimateTokens(t *testing.T) {
	if EstimateTokens("abcd") != 1 {
		t.Fatal("ascii 4 字符≈1 token")
	}
	if EstimateTokens("中中中中") != 2 {
		t.Fatal("cjk 2 字符≈1 token")
	}
	if EstimateTokens("") != 0 {
		t.Fatal("空串为 0")
	}
}
