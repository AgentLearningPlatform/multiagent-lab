// REQ-148 供应商多实例与别名/改名单测：分组回填幂等性 / 同 BaseURL 双实例 / 别名更新 / 连接归属校验。零依赖。
package store

import (
	"path/filepath"
	"testing"
)

func TestProviderGroupBackfillIdempotent(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// 两个 BaseURL 的老数据连接（无 provider_group_id）
	for _, c := range []*ModelConnection{
		{ID: "c1", Name: "DeepSeek·chat", ConnType: "chat", Protocol: "openai_compat", BaseURL: "https://api.deepseek.com/v1", ModelName: "deepseek-chat", Enabled: true},
		{ID: "c2", Name: "DeepSeek·r1", ConnType: "chat", Protocol: "openai_compat", BaseURL: "https://api.deepseek.com/v1", ModelName: "deepseek-r1", Enabled: true},
		{ID: "c3", Name: "GLM·flash", ConnType: "chat", Protocol: "openai_compat", BaseURL: "https://open.bigmodel.cn/api/paas/v4", ModelName: "glm-flash", Enabled: true},
	} {
		if _, err := st.CreateConnection(c, nil); err != nil {
			t.Fatalf("seed %s: %v", c.ID, err)
		}
	}
	if err := st.BackfillProviderGroups(); err != nil {
		t.Fatal(err)
	}
	// 新库自带 011 迁移预置连接，须按 ID 查找断言（ListConnections 全量、按 created_at 排序）
	byID := map[string]*ModelConnection{}
	if err := st.BackfillProviderGroups(); err != nil {
		t.Fatal(err)
	}
	conns, err := st.ListConnections()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range conns {
		byID[c.ID] = c
	}
	if byID["c1"].ProviderGroupID == "" || byID["c3"].ProviderGroupID == "" {
		t.Fatalf("回填后连接应有分组: %+v %+v", byID["c1"], byID["c3"])
	}
	if byID["c1"].ProviderGroupID != byID["c2"].ProviderGroupID {
		t.Fatal("同 BaseURL 的两条连接应同组")
	}
	if byID["c1"].ProviderGroupID == byID["c3"].ProviderGroupID {
		t.Fatal("不同 BaseURL 不应同组")
	}

	// 幂等：重放不新建、不换组
	groups1, _ := st.ListProviderGroups()
	if err := st.BackfillProviderGroups(); err != nil {
		t.Fatal(err)
	}
	groups2, _ := st.ListProviderGroups()
	if len(groups1) != len(groups2) {
		t.Fatalf("重放回填不应新建分组: %d vs %d", len(groups1), len(groups2))
	}
	conns2, _ := st.ListConnections()
	for i := range conns {
		if conns[i].ProviderGroupID != conns2[i].ProviderGroupID {
			t.Fatalf("重放回填不应改变归属: conn%d %s vs %s", i, conns[i].ProviderGroupID, conns2[i].ProviderGroupID)
		}
	}
}

func TestProviderGroupMultiInstanceAndAlias(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// 同 BaseURL 两个独立实例（REQ-148 核心场景：两个账号各自分组）
	g1, err := st.CreateProviderGroup("")
	if err != nil {
		t.Fatal(err)
	}
	g2, err := st.CreateProviderGroup("工作号")
	if err != nil {
		t.Fatal(err)
	}
	if g1.ID == g2.ID {
		t.Fatal("两个实例应有独立分组 ID")
	}
	for _, c := range []*ModelConnection{
		{ID: "a1", Name: "个人号·chat", ConnType: "chat", Protocol: "openai_compat", BaseURL: "https://api.deepseek.com/v1", ModelName: "deepseek-chat", ProviderGroupID: g1.ID},
		{ID: "a2", Name: "工作号·chat", ConnType: "chat", Protocol: "openai_compat", BaseURL: "https://api.deepseek.com/v1", ModelName: "deepseek-chat", ProviderGroupID: g2.ID},
	} {
		if _, err := st.CreateConnection(c, nil); err != nil {
			t.Fatalf("seed %s: %v", c.ID, err)
		}
	}
	// 幂等回填不得把已归属的连接拉回 BaseURL 组
	if err := st.BackfillProviderGroups(); err != nil {
		t.Fatal(err)
	}
	conns, _ := st.ListConnections()
	m := map[string]*ModelConnection{}
	for _, c := range conns {
		m[c.ID] = c
	}
	if m["a1"].ProviderGroupID != g1.ID || m["a2"].ProviderGroupID != g2.ID {
		t.Fatalf("多实例归属被回填破坏: %s / %s", m["a1"].ProviderGroupID, m["a2"].ProviderGroupID)
	}
	if m["a1"].ProviderAlias != "" || m["a2"].ProviderAlias != "工作号" {
		t.Fatalf("别名快照联查不符: %q / %q", m["a1"].ProviderAlias, m["a2"].ProviderAlias)
	}

	// 别名更新（仅展示层，不改连接真名）
	if err := st.UpdateProviderGroup(g1.ID, "个人号"); err != nil {
		t.Fatal(err)
	}
	conns, _ = st.ListConnections()
	m = map[string]*ModelConnection{}
	for _, c := range conns {
		m[c.ID] = c
	}
	if m["a1"].ProviderAlias != "个人号" || m["a1"].Name != "个人号·chat" {
		t.Fatalf("别名更新应只影响展示快照: alias=%q name=%q", m["a1"].ProviderAlias, m["a1"].Name)
	}

	// 不存在的分组
	if err := st.UpdateProviderGroup("pg_missing", "x"); err != ErrNotFound {
		t.Fatalf("更新不存在分组应 ErrNotFound, got %v", err)
	}
}
