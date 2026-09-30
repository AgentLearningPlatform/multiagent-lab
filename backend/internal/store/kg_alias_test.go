// M36/KB-7② 别名列单测：重建/重索引保留人工别名、合并时别名归并、SetKGEntityAlias 归一、
// KGSearchEntities 命中别名。
package store

import (
	"strings"
	"path/filepath"
	"testing"
)

func kgTestEntities(kb string) ([]*KGEntity, []*KGRelationship) {
	return []*KGEntity{
			{ID: NewID(), KBID: kb, DocID: "d1", Name: "工作流引擎", Type: "concept", Description: "调度执行"},
			{ID: NewID(), KBID: kb, DocID: "d1", Name: "大模型", Type: "concept"},
		}, []*KGRelationship{
			{ID: NewID(), KBID: kb, DocID: "d1", Source: "工作流引擎", Target: "大模型", Type: "依赖"},
		}
}

func TestEntityAliasSurvivesRebuild(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	const kb = "kb-alias"
	ents, rels := kgTestEntities(kb)
	if err := st.ReplaceKGForDoc(kb, "", ents, rels, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.SetKGEntityAlias(kb, "工作流引擎", "workflow;编排引擎"); err != nil {
		t.Fatal(err)
	}
	// 重建（KB 级 docID=""）后同名实体别名保留
	ents2, rels2 := kgTestEntities(kb)
	if err := st.ReplaceKGForDoc(kb, "", ents2, rels2, nil); err != nil {
		t.Fatal(err)
	}
	got, _, err := st.KGByKB(kb)
	if err != nil {
		t.Fatal(err)
	}
	var found *KGEntity
	for _, e := range got {
		if e.Name == "工作流引擎" {
			found = e
		}
	}
	if found == nil || !strings.Contains(found.Alias, "workflow") {
		t.Fatalf("重建后别名丢失: %+v", found)
	}
}

func TestEntityAliasMergeCarriesOver(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	const kb = "kb-merge-alias"
	ents := []*KGEntity{
		{ID: NewID(), KBID: kb, DocID: "d1", Name: "短名", Type: "concept"},
		{ID: NewID(), KBID: kb, DocID: "d1", Name: "带别名的长名实体", Type: "concept", Alias: "旧别名"},
	}
	if err := st.ReplaceKGForDoc(kb, "", ents, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.MergeKGEntities(kb, "短名", []string{"带别名的长名实体"}); err != nil {
		t.Fatal(err)
	}
	got, _, err := st.KGByKB(kb)
	if err != nil {
		t.Fatal(err)
	}
	keep := got[0]
	if keep.Name != "短名" || len(got) != 1 {
		t.Fatalf("合并结果异常: %+v", got)
	}
	for _, want := range []string{"带别名的长名实体", "旧别名"} {
		if !strings.Contains(keep.Alias, want) {
			t.Fatalf("keep 别名应含 %q: %q", want, keep.Alias)
		}
	}
}

func TestSetKGEntityAliasNormalizes(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	const kb = "kb-alias-norm"
	ents, rels := kgTestEntities(kb)
	if err := st.ReplaceKGForDoc(kb, "", ents, rels, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.SetKGEntityAlias(kb, "大模型", " LLM ;; 大模型 ;; "); err != nil {
		t.Fatal(err)
	}
	got, _ := st.KGSearchEntities(kb, "LLM", 10)
	if len(got) != 1 || got[0].Name != "大模型" {
		t.Fatalf("别名搜索未命中: %+v", got)
	}
	e, err := st.KGEntitiesByNames(kb, []string{"大模型"})
	if err != nil {
		t.Fatal(err)
	}
	if e["大模型"].Alias != "LLM;大模型" {
		t.Fatalf("别名归一异常: %q", e["大模型"].Alias)
	}
	// 清除别名
	if err := st.SetKGEntityAlias(kb, "大模型", ""); err != nil {
		t.Fatal(err)
	}
	got2, _ := st.KGSearchEntities(kb, "LLM", 10)
	if len(got2) != 0 {
		t.Fatalf("清除后不应命中: %+v", got2)
	}
	// 不存在的实体 → ErrNotFound
	if err := st.SetKGEntityAlias(kb, "不存在", "x"); err != ErrNotFound {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
