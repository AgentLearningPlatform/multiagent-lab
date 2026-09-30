// REQ-214 顺修回归：恢复链 checkpoint 锚点——多级审批第二次恢复不再 checkpoint not exist。
// 场景复现：Run 挂起（id=runID 派生）→ Resume 批准 → 模型再次调工具再次挂起
//（ADK 沿用恢复锚点 id 覆盖保存快照）→ handleInterrupted 须记录同一 id。
package chat

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

func anchorCPID(t *testing.T, s *Service, convID string) string {
	t.Helper()
	conv, err := s.Store.GetConversation(convID)
	if err != nil {
		t.Fatal(err)
	}
	var st interruptState
	if err := json.Unmarshal([]byte(conv.InterruptState), &st); err != nil {
		t.Fatalf("interrupt_state 解析: %v", err)
	}
	return st.CheckpointID
}

func TestInterruptCheckpointAnchor(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/anchor.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Service{Store: st}
	ag, err := st.CreateAgent(&store.Agent{Name: "anchor-agent", MaxIteration: 5})
	if err != nil {
		t.Fatal(err)
	}
	conv, err := st.CreateConversation(&store.Conversation{Scope: "agent", AgentID: &ag.ID, Title: "anchor"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	noemit := func(*Event) {}

	// ① 新 Run 挂起：checkpoint id = runID 派生
	s.handleInterrupted(ctx, conv, "run-1", nil, noemit)
	if got := anchorCPID(t, s, conv.ID); got != "ckpt_run-1" {
		t.Fatalf("新 Run 应 runID 派生, got %s", got)
	}

	// ② Run 清锚点语义：再次 Run 前 cpAnchors 无该会话
	s.cpAnchors.Delete(conv.ID)
	s.handleInterrupted(ctx, conv, "run-2", nil, noemit)
	if got := anchorCPID(t, s, conv.ID); got != "ckpt_run-2" {
		t.Fatalf("清锚点后应 runID 派生, got %s", got)
	}

	// ③ Resume 后再次挂起：ADK 沿用锚点 id 覆盖保存——平台须记录同一 id
	s.cpAnchors.Store(conv.ID, "ckpt_run-2")
	s.handleInterrupted(ctx, conv, "run-3", nil, noemit)
	if got := anchorCPID(t, s, conv.ID); got != "ckpt_run-2" {
		t.Fatalf("恢复链二次挂起应沿用锚点 id, got %s", got)
	}
}
