package store

import (
	"path/filepath"
	"testing"
)

// KB-10①：FTS5 词法索引真库测试（modernc sqlite 内建 FTS5；环境不支持则整组跳过）。
func TestChunkFTS(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "fts.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.DB.Close()
	if !st.ChunkFTSReady() {
		t.Skip("当前环境无 FTS5，跳过（词法臂运行期自动退役）")
	}
	chunks := []*KnowledgeChunk{
		{ID: "c1", KBID: "kb1", DocID: "d1", Seq: 0, Content: "Eino 是字节跳动开源的 Go 大模型应用开发框架，支持链式编排与 ADK 智能体。"},
		{ID: "c2", KBID: "kb1", DocID: "d1", Seq: 1, Content: "Qdrant 是向量数据库，支持 HNSW 索引与 cosine 相似度检索。"},
		{ID: "c3", KBID: "kb1", DocID: "d2", Seq: 0, Content: "BUG-1024 号缺陷：登录页在弱网下超时，修复方案见 KB-1024 设计稿。"},
	}
	if err := st.InsertKnowledgeChunks(chunks); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// 中文子串命中（trigram ≥3 rune）
	hits, err := st.SearchChunksFTS("kb1", FTSQuote("大模型应用开发框架"), 10)
	if err != nil {
		t.Fatalf("search cjk: %v", err)
	}
	if len(hits) == 0 || hits[0].ChunkID != "c1" {
		t.Fatalf("中文词法命中错误: %+v", hits)
	}
	// 编号/术语类（纯向量的弱项）
	hits, err = st.SearchChunksFTS("kb1", FTSQuote("BUG-1024"), 10)
	if err != nil {
		t.Fatalf("search code: %v", err)
	}
	if len(hits) == 0 || hits[0].ChunkID != "c3" {
		t.Fatalf("编号词法命中错误: %+v", hits)
	}
	// 库隔离
	hits, err = st.SearchChunksFTS("kb2", FTSQuote("大模型应用开发框架"), 10)
	if err != nil {
		t.Fatalf("search other kb: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("跨库隔离失败: %+v", hits)
	}
	// 按 doc 删除后命中消失
	st.DeleteChunkFTSByDoc("d1")
	hits, err = st.SearchChunksFTS("kb1", FTSQuote("大模型应用开发框架"), 10)
	if err != nil {
		t.Fatalf("search after delete: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("按 doc 删除后仍命中: %+v", hits)
	}
}

// KB-10①：自然语言问句经 FTSMatchQuery 关键词化后应命中（整句短语匹配对问句必失配的回归）。
func TestFTSMatchQueryNaturalLanguage(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "fts2.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.DB.Close()
	if !st.ChunkFTSReady() {
		t.Skip("当前环境无 FTS5，跳过")
	}
	chunks := []*KnowledgeChunk{
		{ID: "n1", KBID: "kb1", DocID: "d1", Seq: 0, Content: "BUG-1024：检索服务在弱网环境下 P99 延迟超过 30 秒，定位为 Qdrant 客户端超时未配置；修复方案是显式设置 3 秒拨号超时。"},
		{ID: "n2", KBID: "kb1", DocID: "d1", Seq: 1, Content: "Eino 框架的 Retriever 组件经 eino-ext 封装后可直连 Qdrant 集群。"},
	}
	if err := st.InsertKnowledgeChunks(chunks); err != nil {
		t.Fatalf("insert: %v", err)
	}
	hits, err := st.SearchChunksFTS("kb1", FTSMatchQuery("BUG-1024 弱网超时的修复方案是什么"), 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 || hits[0].ChunkID != "n1" {
		t.Fatalf("自然语言问句应命中 n1: %+v", hits)
	}
	hits, err = st.SearchChunksFTS("kb1", FTSMatchQuery("Eino 框架直连哪个集群"), 5)
	if err != nil {
		t.Fatalf("search 2: %v", err)
	}
	if len(hits) == 0 || hits[0].ChunkID != "n2" {
		t.Fatalf("含锚点词的问句应命中 n2: %+v", hits)
	}
}
