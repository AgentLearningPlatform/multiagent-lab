package api

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

func newCfgVerFixture(t *testing.T) *Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return &Server{Store: st}
}

// REQ-226/M54：保存即快照→列表→diff→rollback→再快照 全链（store 面）。
func TestAgentConfigVersionLifecycle(t *testing.T) {
	s := newCfgVerFixture(t)
	ag, err := s.Store.CreateAgent(&store.Agent{Name: "cv", Instruction: "v0 指令"})
	if err != nil {
		t.Fatal(err)
	}
	cur, _ := s.Store.GetAgent(ag.ID)
	curJSON, _ := json.Marshal(cur)
	// 两次「保存前」快照
	if _, err := s.Store.InsertAgentConfigVersion(ag.ID, string(curJSON), "保存前自动快照"); err != nil {
		t.Fatal(err)
	}
	// 模拟配置变更后再快照
	updated := *cur
	updated.Instruction = "v1 指令"
	if _, err := s.Store.UpdateAgent(&updated); err != nil {
		t.Fatal(err)
	}
	cur2, _ := s.Store.GetAgent(ag.ID)
	cur2JSON, _ := json.Marshal(cur2)
	v2, err := s.Store.InsertAgentConfigVersion(ag.ID, string(cur2JSON), "保存前自动快照")
	if err != nil {
		t.Fatal(err)
	}
	if v2.Version != 2 {
		t.Fatalf("版本号应单调递增，got %d", v2.Version)
	}
	// 列表新→旧
	list, _ := s.Store.ListAgentConfigVersions(ag.ID)
	if len(list) != 2 || list[0].Version != 2 {
		t.Fatalf("列表应 2 条新→旧: %d", len(list))
	}
	// diff：v1 vs 当前（instruction 变了）
	diffs, err := store.AgentConfigDiff(list[1].ConfigJSON, string(cur2JSON))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, d := range diffs {
		if d["field"] == "instruction" && d["old"] == "v0 指令" && d["new"] == "v1 指令" {
			found = true
		}
	}
	if !found {
		t.Fatalf("diff 应含 instruction 变更: %v", diffs)
	}
	// 回滚到 v1：instruction 回到 v0；且回滚前自动快照落 v3
	v1, _ := s.Store.GetAgentConfigVersion(ag.ID, 1)
	var target store.Agent
	if err := json.Unmarshal([]byte(v1.ConfigJSON), &target); err != nil {
		t.Fatal(err)
	}
	target.ID = ag.ID
	if _, err := s.Store.UpdateAgent(&target); err != nil {
		t.Fatal(err)
	}
	curNow, _ := s.Store.GetAgent(ag.ID)
	if curNow.Instruction != "v0 指令" {
		t.Fatalf("回滚后 instruction 应为 v0: %s", curNow.Instruction)
	}
	// retention：灌 55 版只留 50
	for i := 0; i < 55; i++ {
		_, _ = s.Store.InsertAgentConfigVersion(ag.ID, string(cur2JSON), "灌")
	}
	list2, _ := s.Store.ListAgentConfigVersions(ag.ID)
	if len(list2) > 50 {
		t.Fatalf("retention 应 ≤50, got %d", len(list2))
	}
}
