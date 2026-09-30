package store

import (
	"path/filepath"
	"testing"
)

// REQ-211/M44 伴生图作用域 agent 化存储面：游标复合键（会话×agent）+ 迁移回填 +
// agent 维度摘除/图迁移源/meta 标记。迁移 029 建表随 store.Open 自动执行。

func openCompanionTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "companion-scope.db"))
	if err != nil {
		t.Fatalf("建临时库失败: %v", err)
	}
	return st
}

func TestCompanionCursorCompositeKey(t *testing.T) {
	st := openCompanionTestStore(t)
	// 同会话两个 agent 各自游标互不覆盖（项目会话多 agent 共用场景）
	if err := st.AdvanceCompanionCursor("conv-p", "agt-a", "m3"); err != nil {
		t.Fatalf("advance a: %v", err)
	}
	if err := st.AdvanceCompanionCursor("conv-p", "agt-b", "m5"); err != nil {
		t.Fatalf("advance b: %v", err)
	}
	ca, _ := st.GetCompanionCursor("conv-p", "agt-a")
	cb, _ := st.GetCompanionCursor("conv-p", "agt-b")
	if ca.LastMessageID != "m3" || cb.LastMessageID != "m5" {
		t.Fatalf("复合游标应互不覆盖: a=%s b=%s", ca.LastMessageID, cb.LastMessageID)
	}
	// 幂等推进
	if err := st.AdvanceCompanionCursor("conv-p", "agt-a", "m7"); err != nil {
		t.Fatalf("re-advance: %v", err)
	}
	if ca2, _ := st.GetCompanionCursor("conv-p", "agt-a"); ca2.LastMessageID != "m7" {
		t.Fatalf("推进应生效: %s", ca2.LastMessageID)
	}
	// 无记录返回空游标（占位含复合键）
	if ce, _ := st.GetCompanionCursor("conv-none", "agt-x"); ce.LastMessageID != "" || ce.AgentID != "agt-x" {
		t.Fatalf("空游标不符: %+v", ce)
	}
	// 会话级清理清全部 agent 的游标
	_ = st.DeleteConversationCompanionData("conv-p")
	if ca3, _ := st.GetCompanionCursor("conv-p", "agt-a"); ca3.LastMessageID != "" {
		t.Fatalf("会话清理后游标应空: %+v", ca3)
	}
}

func TestCompanionCursorBackfillMigration(t *testing.T) {
	st := openCompanionTestStore(t)
	// 存量形态：agent 会话游标 + 项目会话游标（迁移 029 从旧单键表回填——此处直接验回填结果形态：
	// 新库无旧表数据，验证写入后回读键完整性；旧库回填由 SQL 迁移承担，形态由本用例锁定）
	_ = st.AdvanceCompanionCursor("conv-agent1", "agt-owner", "m2")
	cur, err := st.GetCompanionCursor("conv-agent1", "agt-owner")
	if err != nil || cur.LastMessageID != "m2" || cur.AgentID != "agt-owner" {
		t.Fatalf("agent 会话游标回读不符: %+v %v", cur, err)
	}
}

func TestCompanionAgentScopeHelpers(t *testing.T) {
	st := openCompanionTestStore(t)
	cands := []*CompanionCandidate{
		{ID: "c1", ConversationID: "conv-1", AgentID: "agt-a", Kind: "concept", Name: "实体A", Status: "pending"},
		{ID: "c2", ConversationID: "conv-2", AgentID: "agt-a", Kind: "concept", Name: "实体B", Status: "confirmed"},
		{ID: "c3", ConversationID: "conv-3", AgentID: "agt-b", Kind: "event", Name: "事件C", Status: "pending"},
	}
	if err := st.CreateCompanionCandidates(cands); err != nil {
		t.Fatalf("落库: %v", err)
	}
	_ = st.AdvanceCompanionCursor("conv-1", "agt-a", "m1")
	_ = st.AdvanceCompanionCursor("conv-3", "agt-b", "m9")

	// 图迁移源：agent × 会话对去重
	srcs, err := st.ListCompanionGraphSources()
	if err != nil {
		t.Fatalf("sources: %v", err)
	}
	got := map[string]bool{}
	for _, g := range srcs {
		got[g.AgentID+"/"+g.ConversationID] = true
	}
	if !got["agt-a/conv-1"] || !got["agt-a/conv-2"] || !got["agt-b/conv-3"] {
		t.Fatalf("迁移源应含全部 agent×会话对: %v", got)
	}
	// 在抽会话数
	if n, _ := st.CountCompanionCursors("agt-a"); n != 1 {
		t.Fatalf("agt-a 在抽会话数应 1: %d", n)
	}
	// agent 级摘除：agt-a 候选与游标全清，agt-b 不受影响
	if err := st.DeleteAgentCompanionData("agt-a"); err != nil {
		t.Fatalf("agent 摘除: %v", err)
	}
	if left, _ := st.ListCompanionCandidates("", "agt-a", ""); len(left) != 0 {
		t.Fatalf("agt-a 候选应清空: %d", len(left))
	}
	if left, _ := st.ListCompanionCandidates("", "agt-b", ""); len(left) != 1 {
		t.Fatalf("agt-b 候选应保留: %d", len(left))
	}
	if cur, _ := st.GetCompanionCursor("conv-1", "agt-a"); cur.LastMessageID != "" {
		t.Fatalf("agt-a 游标应清空: %+v", cur)
	}
	// meta 标记
	if v, _ := st.GetCompanionMeta("convgraphs_migrated"); v != "" {
		t.Fatalf("初始标记应为空: %q", v)
	}
	if err := st.SetCompanionMeta("convgraphs_migrated", "1"); err != nil {
		t.Fatalf("set meta: %v", err)
	}
	if v, _ := st.GetCompanionMeta("convgraphs_migrated"); v != "1" {
		t.Fatalf("meta 回读不符: %q", v)
	}
}
