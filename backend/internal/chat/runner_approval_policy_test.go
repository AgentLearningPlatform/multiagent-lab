package chat

import (
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// REQ-231④：生效审批策略解析六象限（与装配期合并同语义；run.started 透出口径）。
func TestEffectiveApprovalPolicy(t *testing.T) {
	convAp := func(v string) *string { return &v }
	cases := []struct {
		name          string
		agent         string
		conv          *string
		wantMode      string
		wantSource    string
	}{
		{"agent off + 无覆盖", "", nil, "", "agent"},
		{"agent all + 无覆盖", "all", nil, "all", "agent"},
		{"agent danger + 无覆盖", "danger", nil, "danger", "agent"},
		{"agent off + 会话 on 升 all", "", convAp("on"), "all", "conversation"},
		{"agent danger + 会话 on 保留 danger", "danger", convAp("on"), "danger", "conversation"},
		{"agent all + 会话 off 关闭", "all", convAp("off"), "", "conversation"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mode, source := effectiveApprovalPolicy(&store.Agent{ToolApproval: c.agent}, &store.Conversation{ToolApproval: c.conv})
			if mode != c.wantMode || source != c.wantSource {
				t.Fatalf("got mode=%q source=%q, want mode=%q source=%q", mode, source, c.wantMode, c.wantSource)
			}
		})
	}
}
