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
