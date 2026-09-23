package kb

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// ---- GraphRAG 子模块（M14 D-KB4 建制；D-O15/REQ-110 起自研实现）----
//
// graphrag 模式的 KB 在向量索引（chunks/embedding，与 rag 共用）之外，把 chunk 抽取为自存 KG
// （store 表 kg_entity / kg_relationship / kg_claim）。抽取器由 api 层注入（internal/kg：
// REQ-98 LLM 能力代理主路径 + 规则抽取回退），kb 包不依赖 chat（chat → kb 已有依赖，反向会成环）。
// 检索（教学口径「前期够教学」）：向量命中 chunk → 提及实体 → KG 一跳扩展 → 实体描述 + claims 拼上下文。
// 任何失败统一降级（M14 ⑥ 语义保留）：抽取 degraded 不阻断导入，检索回退纯向量。

// KGExtractFunc KG 抽取注入签名（docID 为空 = KB 级重建）。
type KGExtractFunc func(ctx context.Context, kbID, docID string, chunks []*store.KnowledgeChunk) *store.GraphragInfo

// SetKGExtractor api 层装配（Service 构造后立即调用；不装配则 graphrag 抽取退化为 degraded，rag 模式不受影响）。
func (s *Service) SetKGExtractor(fn KGExtractFunc) { s.kgExtract = fn }

// GraphragIngest graphrag 模式文档索引成功后抽取 KG 并自存（M14 ② 槽位不变，实现切自研）。
// 永不返回 error：失败封装为 degraded 结果（非阻断，导入主流程已成功）。
// docID 推断：单文档池（Import/Reindex）取 chunks[0].DocID；跨文档池（KB 级重建）置空 = 全量替换。
func (s *Service) GraphragIngest(ctx context.Context, kbID string, chunks []*store.KnowledgeChunk) *store.GraphragInfo {
	if s.kgExtract == nil {
		return &store.GraphragInfo{Degraded: true, Error: "KG 抽取器未装配（内部装配错误）"}
	}
	docID := ""
	if len(chunks) > 0 {
		same := true
		for _, c := range chunks {
			if c.DocID != chunks[0].DocID {
				same = false
				break
			}
		}
		if same {
			docID = chunks[0].DocID
		}
	}
	info := s.kgExtract(ctx, kbID, docID, chunks)
	if info.OK { // 审计留痕（REQ-101 消费/审计页素材）：抽取方式与规模本身即一条决策
		_, err := s.Store.InsertDecision(&store.OntoDecision{
			SubjectKind: "kg",
			SubjectID:   kbID,
			Title:       "KG 抽取（method=" + info.Method + "）",
			Rationale:   "graphrag 模式文档索引联动：chunks=" + strconv.Itoa(info.Chunks) + "，实体=" + strconv.Itoa(info.Entities) + "，关系=" + strconv.Itoa(info.Relationships),
			MetaJSON: fmt.Sprintf(`{"method":%q,"chunks":%d,"entities":%d,"relationships":%d}`,
				info.Method, info.Chunks, info.Entities, info.Relationships),
		})
		if err != nil {
			log.Printf("[kb] warn: 记录 KG 决策失败 (kb=%s): %v", kbID, err)
		}
	}
	return info
}

// errKGEmpty KG 无覆盖（未抽取或实体未命中该问题）→ 回退向量检索的内部信号。
var errKGEmpty = fmt.Errorf("KG 无命中")

// GraphragQuery 自研 GraphRAG 检索（D-O15 三步：向量命中 → KG 一跳扩展 → 拼上下文）。
func (s *Service) GraphragQuery(ctx context.Context, k *store.KnowledgeBase, query string, maxResults int) ([]RetrievalHit, error) {
	if query == "" {
		return nil, nil
	}
	if maxResults <= 0 {
		maxResults = k.TopK
	}
	if maxResults <= 0 {
		maxResults = 4
	}
	// ① 向量命中（与 rag 共用通道；score 沿用向量相似度）
	vec, err := s.embedder().EmbedOne(ctx, query)
	if err != nil {
		return nil, err
	}
	seeds, err := s.Vector.Search(ctx, k.ID, vec, maxResults, 0)
	if err != nil {
		return nil, err
	}
	if len(seeds) == 0 {
		return []RetrievalHit{}, nil
	}
	chunkIDs := make([]string, 0, len(seeds))
	scoreByChunk := map[string]float64{}
	for _, h := range seeds {
		chunkIDs = append(chunkIDs, h.ChunkID)
		scoreByChunk[h.ChunkID] = h.Score
	}
	// ② 命中 chunk → 提及实体 → 一跳关系扩展
	subjects, err := s.Store.KGSubjectsByChunks(k.ID, chunkIDs)
	if err != nil {
		return nil, err
	}
	if len(subjects) == 0 {
		return nil, errKGEmpty
	}
	rels, err := s.Store.KGNeighbors(k.ID, subjects)
	if err != nil {
		return nil, err
	}
	nameSet := map[string]bool{}
	for _, r := range rels {
		nameSet[r.Source] = true
		nameSet[r.Target] = true
	}
	names := make([]string, 0, len(nameSet))
	for n := range nameSet {
		names = append(names, n)
	}
	entities, err := s.Store.KGEntitiesByNames(k.ID, names)
	if err != nil {
		return nil, err
	}
	claims, err := s.Store.KGClaimsForSubjects(k.ID, names, maxResults*6)
	if err != nil {
		return nil, err
	}
	// ③ 拼上下文：seed 实体（score 1.0）优先，一跳邻居（0.6）次之，claims（继承 seed chunk 分数）补事实
	subjectSet := map[string]bool{}
	for _, n := range subjects {
		subjectSet[n] = true
	}
	type expansion struct {
		name  string
		score float64
	}
	expansions := []expansion{}
	seenName := map[string]bool{}
	for _, r := range rels {
		for _, n := range []string{r.Source, r.Target} {
			if seenName[n] {
				continue
			}
			seenName[n] = true
			sc := 0.6
			if subjectSet[n] {
				sc = 1.0
			}
			expansions = append(expansions, expansion{name: n, score: sc})
		}
	}
	sort.Slice(expansions, func(i, j int) bool { return expansions[i].score > expansions[j].score })
	hits := make([]RetrievalHit, 0, maxResults)
	for _, e := range expansions {
		if len(hits) >= maxResults {
			break
		}
		excerpt := "(未填写描述)"
		if ent := entities[e.name]; ent != nil && ent.Description != "" {
			excerpt = truncateRunes(ent.Description, 200)
		}
		hits = append(hits, RetrievalHit{Doc: k.Name + " · " + e.name, Seq: len(hits), Score: e.score, Excerpt: excerpt})
	}
	for _, c := range claims {
		if len(hits) >= maxResults {
			break
		}
		hits = append(hits, RetrievalHit{Doc: k.Name + " · " + c.Subject, Seq: len(hits),
			Score: claimScore(scoreByChunk, c.ChunkID), Excerpt: truncateRunes(c.Text, 200)})
	}
	return hits, nil
}

// claimScore claim 分数继承其出处 chunk 的向量命中分（无溯源 chunk 时给固定值）。
func claimScore(scoreByChunk map[string]float64, chunkID string) float64 {
	if chunkID == "" {
		return 0.8
	}
	if v, ok := scoreByChunk[chunkID]; ok && v > 0 {
		return v
	}
	return 0.8
}

// KGEntity / KGRelationship KG 读回元素（O13 策略 B/C 数据源；D-O15 起读自存表）。
type KGEntity struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
	Desc string `json:"desc,omitempty"` // 轻量抽取/LLM 捕获的首个描述句
}

type KGRelationship struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type,omitempty"`
}

// KGData 一个 KB 的 KG 子图。
type KGData struct {
	KBID          string           `json:"kb_id"`
	Method        string           `json:"method,omitempty"` // llm | lightweight（最近一次抽取决策反查）
	Entities      []KGEntity       `json:"entities"`
	Relationships []KGRelationship `json:"relationships"`
}

// GraphragKG 读回某 KB 的 KG 子图（O13 ④ kg-to-spec-json 与策略 B/C 的数据源；自存表直读）。
func (s *Service) GraphragKG(ctx context.Context, kbID string) (*KGData, error) {
	entityRows, relRows, err := s.Store.KGByKB(kbID)
	if err != nil {
		return nil, err
	}
	out := &KGData{KBID: kbID, Entities: []KGEntity{}, Relationships: []KGRelationship{}}
	for _, e := range entityRows {
		out.Entities = append(out.Entities, KGEntity{ID: e.ID, Name: e.Name, Type: e.Type, Desc: e.Description})
	}
	for _, r := range relRows {
		out.Relationships = append(out.Relationships, KGRelationship{Source: r.Source, Target: r.Target, Type: r.Type})
	}
	// method 反查最近一条 kg 决策（GraphragIngest 落痕；无记录留空，不影响映射）
	if decisions, derr := s.Store.ListDecisions("kg", kbID, 1); derr == nil && len(decisions) > 0 {
		var m struct {
			Method string `json:"method"`
		}
		if json.Unmarshal([]byte(decisions[0].MetaJSON), &m) == nil {
			out.Method = m.Method
		}
	}
	return out, nil
}

// GraphragQueryWithFallback M14 ⑥：graphrag 检索失败/无命中 → 回退向量检索，返回 (hits, mode, degraded, err)。
func (s *Service) GraphragQueryWithFallback(ctx context.Context, k *store.KnowledgeBase, query string, maxResults int, minScore float64) ([]RetrievalHit, string, bool, error) {
	if k.Mode != "graphrag" {
		hits, err := s.Search(ctx, k, query, maxResults, minScore)
		return hits, "rag", false, err
	}
	hits, err := s.GraphragQuery(ctx, k, query, maxResults)
	if err == nil && len(hits) > 0 {
		return hits, "graphrag", false, nil
	}
	if err != nil {
		log.Printf("[kb] graphrag query degraded (kb=%s): %v → 回退向量检索", k.ID, err)
	} else {
		log.Printf("[kb] graphrag query 无命中 (kb=%s) → 回退向量检索", k.ID)
	}
	fb, ferr := s.Search(ctx, k, query, maxResults, minScore)
	return fb, "rag", true, ferr
}
