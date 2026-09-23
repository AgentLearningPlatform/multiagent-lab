// M16 阶段二（REQ-129）治理链路单测：审核状态过滤检索 / 实体消歧合并 / 质量面板 / 合并建议。零依赖。
package store

import (
	"path/filepath"
	"testing"
)

func seedGovernanceKG(t *testing.T, st *Store, kbID string) {
	t.Helper()
	ents := []*KGEntity{
		{ID: "e1", KBID: kbID, DocID: "d1", Name: "编排引擎", Type: "concept"},
		{ID: "e2", KBID: kbID, DocID: "d1", Name: "智能体", Type: "concept"},
		{ID: "e3", KBID: kbID, DocID: "d1", Name: "智能体系统", Type: "concept"}, // 与"智能体"构成包含对（合并建议）
		{ID: "e4", KBID: kbID, DocID: "d1", Name: "孤立实体", Type: "concept"},  // 无任何关系边
	}
	rels := []*KGRelationship{
		{ID: "r1", KBID: kbID, DocID: "d1", Source: "编排引擎", Target: "智能体", Type: "调度"},
		{ID: "r2", KBID: kbID, DocID: "d1", Source: "智能体", Target: "智能体系统", Type: "属于"},
	}
	claims := []*KGClaim{
		{ID: "c1", KBID: kbID, DocID: "d1", ChunkID: "ch1", Subject: "编排引擎", Text: "编排引擎负责调度"},
		{ID: "c2", KBID: kbID, DocID: "d1", ChunkID: "ch2", Subject: "智能体系统", Text: "智能体系统是平台形态"},
	}
	if err := st.ReplaceKGForDoc(kbID, "", ents, rels, claims); err != nil {
		t.Fatal(err)
	}
}

func TestKGReviewFiltersRetrieval(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	const kb = "kb"
	seedGovernanceKG(t, st, kb)

	// reject r1（编排引擎→智能体）后，一跳扩展不再含该边
	if err := st.SetKGRelStatus(kb, "r1", "rejected"); err != nil {
		t.Fatal(err)
	}
	rels, err := st.KGNeighbors(kb, []string{"编排引擎"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 0 {
		t.Fatalf("rejected rel still retrieved: %+v", rels)
	}
	// 恢复 approved 后重新可见
	if err := st.SetKGRelStatus(kb, "r1", "approved"); err != nil {
		t.Fatal(err)
	}
	if rels, _ = st.KGNeighbors(kb, []string{"编排引擎"}); len(rels) != 1 {
		t.Fatalf("approved rel missing: %+v", rels)
	}
	// claim 审核同理
	if err := st.SetKGClaimStatus(kb, "c1", "rejected"); err != nil {
		t.Fatal(err)
	}
	clms, _ := st.KGClaimsForSubjects(kb, []string{"编排引擎"}, 10)
	if len(clms) != 0 {
		t.Fatalf("rejected claim still retrieved: %+v", clms)
	}
	// 非法状态值拒绝
	if err := st.SetKGClaimStatus(kb, "c1", "maybe"); err == nil {
		t.Fatal("want error for invalid status")
	}
}

func TestKGMergeAndQuality(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	const kb = "kb"
	seedGovernanceKG(t, st, kb)

	// 合并建议：智能体 ↔ 智能体系统（包含关系）
	sugs, err := st.KGMergeSuggestions(kb)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, sg := range sugs {
		if (sg.Keep == "智能体" && sg.Merge == "智能体系统") || (sg.Keep == "智能体系统" && sg.Merge == "智能体") {
			found = true
		}
	}
	if !found {
		t.Fatalf("merge suggestions missing 智能体 pair: %+v", sugs)
	}

	// 执行合并：智能体系统 → 智能体（r2/c2 迁移，实体删除）
	mr, mc, err := st.MergeKGEntities(kb, "智能体", []string{"智能体系统"})
	if err != nil {
		t.Fatal(err)
	}
	if mr != 1 || mc != 1 {
		t.Fatalf("moved rels=%d claims=%d, want 1/1", mr, mc)
	}
	allEnts, allRels, _ := st.KGByKB(kb)
	for _, e := range allEnts {
		if e.Name == "智能体系统" {
			t.Fatal("merged entity still exists")
		}
	}
	// 自环清理验证：不应出现 source=target 的边
	for _, r := range allRels {
		if r.Source == r.Target {
			t.Fatalf("self loop after merge: %+v", r)
		}
	}

	// 质量面板：rejected 计数 + method 分布（无决策行时为空 map）+ 孤儿实体（孤立实体）
	if err := st.SetKGRelStatus(kb, "r1", "rejected"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertDecision(&OntoDecision{SubjectKind: "kg", SubjectID: kb,
		Title: "KG 抽取（method=llm）", MetaJSON: `{"method":"llm"}`}); err != nil {
		t.Fatal(err)
	}
	q, err := st.KGQuality(kb)
	if err != nil {
		t.Fatal(err)
	}
	if q.MethodDist["llm"] != 1 {
		t.Fatalf("method dist = %+v", q.MethodDist)
	}
	if q.RejectedRels != 1 || q.RejectedClms != 0 {
		t.Fatalf("rejected counts = %+v", q)
	}
	if q.OrphanEntity != 1 { // 孤立实体
		t.Fatalf("orphan = %d, want 1", q.OrphanEntity)
	}
}
