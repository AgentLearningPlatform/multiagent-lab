package chat

import (
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// M-O14 阶段三：L1 提案暂存/取用/TTL——两段式确认的存储侧契约。
func TestAssistantProposalLifecycle(t *testing.T) {
	cur := &store.AssistantConfig{SystemPrompt: "默认提示词", ModelConnID: "conn_a"}
	prop := &store.AssistantConfig{SystemPrompt: "优化后的提示词", ModelConnID: "conn_b", Temperature: floatPtr(0.7)}

	p := StageAssistantProposal(cur, prop)
	if p.ID == "" || len(p.Changes) != 3 {
		t.Fatalf("提案暂存不符: %+v", p.Changes)
	}
	// Peek 不消费
	if got := PeekAssistantProposal(); got == nil || got.ID != p.ID {
		t.Fatal("Peek 未取到最新提案")
	}
	// Take 一次性
	if got := TakeAssistantProposal(p.ID); got == nil {
		t.Fatal("Take 未取到提案")
	}
	if got := TakeAssistantProposal(p.ID); got != nil {
		t.Fatal("Take 应一次性（第二次应为空）")
	}
	if got := PeekAssistantProposal(); got != nil {
		t.Fatal("取用后 Peek 应为空")
	}
	// 无差异提案不产生变更
	same := StageAssistantProposal(cur, &store.AssistantConfig{SystemPrompt: "默认提示词"})
	if len(same.Changes) != 0 {
		t.Fatalf("无差异提案不应产生 changes: %+v", same.Changes)
	}
}

func floatPtr(f float64) *float64 { return &f }
