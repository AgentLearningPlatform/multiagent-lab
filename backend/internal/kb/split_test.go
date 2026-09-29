package kb

import (
	"strings"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// KB-10②：结构感知切分——md 标题节=父块、节内切子块、短节自父；纯文本窗口聚合父块。
func TestSplitPieces(t *testing.T) {
	md := "# 总览\n" + strings.Repeat("总览段落内容。", 120) + "\n\n## 检索\n" + strings.Repeat("检索段落内容。", 20) + "\n\n## 治理\n治理很短。"
	pieces := SplitPieces(md)
	if len(pieces) < 3 {
		t.Fatalf("md 切分应 ≥3 块, got %d", len(pieces))
	}
	// 「## 治理」短节应单块自父（内容含标题）
	foundShort := false
	for _, p := range pieces {
		if strings.HasPrefix(p.Content, "## 治理") {
			foundShort = true
			if p.Parent != "" {
				t.Fatalf("短节应自父（Parent 空）: %q", p.Parent)
			}
		}
	}
	if !foundShort {
		t.Fatal("未找到「## 治理」块")
	}
	// 大节应有父子块：子块 Parent 含对应标题
	foundChild := false
	for _, p := range pieces {
		if p.Parent != "" && strings.Contains(p.Parent, "总览") && strings.HasPrefix(p.Content, "总览") == false {
			foundChild = true
		}
	}
	if !foundChild {
		t.Fatal("大节未产生父子块")
	}
	// 纯文本：子块 + 聚合父块
	plain := strings.Repeat("无结构长文本内容，用于验证聚合父块。", 100)
	pp := SplitPieces(plain)
	if len(pp) < 2 {
		t.Fatalf("纯文本应切多块, got %d", len(pp))
	}
	parents := map[string]bool{}
	for _, p := range pp {
		if p.Parent == "" {
			t.Fatal("纯文本子块应有聚合父块")
		}
		parents[p.Parent] = true
	}
	if len(parents) > 3 {
		t.Fatalf("父块聚合过多: %d", len(parents))
	}
	// 短文本：单块自父
	if got := SplitPieces("很短的一段话。"); len(got) != 1 || got[0].Parent != "" {
		t.Fatalf("短文本应单块自父: %+v", got)
	}
}

// KB-11：能力开关归一——两者皆 false 按 mode 派生；scanKB 回读布尔。
func TestKBCapabilityNormalize(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/cap.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.DB.Close()
	// 建库未声明能力 → 按 mode 派生
	rag, err := st.CreateKnowledgeBase(&store.KnowledgeBase{Name: "r1", Mode: "rag"})
	if err != nil {
		t.Fatal(err)
	}
	if !rag.KBVector || rag.KBGraph {
		t.Fatalf("rag 默认应仅向量: %+v", rag)
	}
	gr, err := st.CreateKnowledgeBase(&store.KnowledgeBase{Name: "g1", Mode: "graphrag"})
	if err != nil {
		t.Fatal(err)
	}
	if gr.KBVector || !gr.KBGraph {
		t.Fatalf("graphrag 默认应仅图谱: %+v", gr)
	}
	// 更新为双能力开
	up, err := st.UpdateKnowledgeBase(&store.KnowledgeBase{ID: rag.ID, Name: "r1", Mode: "rag", TopK: rag.TopK, MinScore: rag.MinScore, KBVector: true, KBGraph: true})
	if err != nil {
		t.Fatal(err)
	}
	if !up.KBVector || !up.KBGraph {
		t.Fatalf("双能力开应保持: %+v", up)
	}
	// 全部关闭 → 归一按 mode 派生
	back, err := st.UpdateKnowledgeBase(&store.KnowledgeBase{ID: up.ID, Name: "r1", Mode: "rag", TopK: rag.TopK, MinScore: rag.MinScore})
	if err != nil {
		t.Fatal(err)
	}
	if !back.KBVector || back.KBGraph {
		t.Fatalf("全关应归一为 mode 派生: %+v", back)
	}
}
