package kb

import (
	"context"
	"encoding/binary"
	"math"
	"sort"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// SQLiteStore fallback 后端（§6.9：沿用 SQLiteRetriever 余弦 TopK，无外部依赖对照）。
// 向量以 float32 小端 BLOB 存 knowledge_chunk.vector，检索全量加载计算余弦。
type SQLiteStore struct {
	ST *store.Store
}

// EncodeVector float32 切片 → 小端 BLOB。
func EncodeVector(vec []float32) []byte {
	b := make([]byte, 4*len(vec))
	for i, v := range vec {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(v))
	}
	return b
}

// DecodeVector 小端 BLOB → float32 切片。
func DecodeVector(b []byte) []float32 {
	if len(b) < 4 {
		return nil
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

// EnsureCollection SQLite 无需建集合。
func (SQLiteStore) EnsureCollection(context.Context, string, int) error { return nil }

// Upsert SQLite 路径向量随 chunk 落库（Service 层处理），此处无操作。
func (SQLiteStore) Upsert(context.Context, string, []Chunk) error { return nil }

// Search 余弦 TopK。
func (s SQLiteStore) Search(_ context.Context, kbID string, vec []float32, topK int, minScore float64) ([]Hit, error) {
	chunks, err := s.ST.SearchChunksSQLite(kbID)
	if err != nil {
		return nil, err
	}
	type scored struct {
		c *store.KnowledgeChunk
		s float64
	}
	var all []scored
	for _, c := range chunks {
		v := DecodeVector(c.Vector)
		if len(v) == 0 || len(v) != len(vec) {
			continue
		}
		sim := cosine(vec, v)
		if minScore > 0 && sim < minScore {
			continue
		}
		all = append(all, scored{c, sim})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].s > all[j].s })
	if topK > 0 && len(all) > topK {
		all = all[:topK]
	}
	out := make([]Hit, 0, len(all))
	for _, x := range all {
		out = append(out, Hit{ChunkID: x.c.ID, DocID: x.c.DocID, Seq: x.c.Seq, Content: x.c.Content, Score: x.s})
	}
	return out, nil
}

// DeleteByDoc SQLite 路径向量随 chunk 删除（Service 层处理），此处无操作。
func (SQLiteStore) DeleteByDoc(context.Context, string, string) error { return nil }

// cosine 余弦相似度（假设已归一化或通用计算均可）。
func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
