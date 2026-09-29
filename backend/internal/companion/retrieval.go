package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/kb"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// ---------------------------------------------------------------------------
// REQ-170 P2「KG 检索源并入」+ REQ-194/M34 批次一②召回增强（治 G1 词法召回恰是隐式召回失败）：
// 三级召回链——a) 向量主（标签批量 embedding 余弦 topK≤5 阈值 0.35，全局默认 embedding 连接
// REQ-46 机制）→ b) 词法兜底（recallEntities 双向包含，原样保留）→ 合并去重（向量优先占坑）；
// 命中实体拉 2 跳邻域（bot:Relation 边节点两跳，每实体 ≤8 边，2 跳边标 hop=2）；
// retrieval 事件明细带 match 字段（vector/lexical/vector+lexical，诚实呈现召回来源）。
// 降级链：embedding 未配置/失败 → 静默降级词法 + 日志（与 KB 召回同口径，不发 run.warning）；
// 两路全空 = 空上下文不阻断主链路。chat.CompanionSource 接口与 SSE 契约零改动。
// ---------------------------------------------------------------------------

// maxRecallEntities 单次并入检索的命中实体上限（薄版口径：少而准，避免上下文淹没）。
const maxRecallEntities = 5

// vecMatchThreshold 向量召回相似度阈值（可调常量；经验起点 0.35，基准 evaldata 可校准）。
const vecMatchThreshold = 0.35

// maxEntityEdges 命中实体邻域边总量限流（1 跳优先，2 跳补位）。
const maxEntityEdges = 8

// edgeRow 一条活跃关系边的渲染行（hop=2 为 2 跳链式边，relName 为拼链文本）。
type edgeRow struct {
	Rel   string `json:"rel"`
	Other string `json:"other"`
	Dir   string `json:"dir"` // out=主体→他者 | in=他者→主体
	Hop   int    `json:"hop,omitempty"`
}

// entityHit 命中实体明细（retrieval 事件与上下文渲染共用）。
type entityHit struct {
	Label       string    `json:"label"`
	Definition  string    `json:"definition,omitempty"`
	Confidence  float64   `json:"confidence,omitempty"`
	Match       string    `json:"match,omitempty"` // vector | lexical | vector+lexical（REQ-194②如实呈现）
	HasEdgeInfo bool      `json:"-"`
	Edges       []edgeRow `json:"relations,omitempty"`
}

// RetrievalContext 实现 chat.CompanionSource（接口反转注入，companion→chat 包环约束）。
// 返回值：注入文本（空=无命中）、实体明细（retrieval 事件用）、错误（仅引擎通信失败）。
func (s *Service) RetrievalContext(ctx context.Context, conv *store.Conversation, input string) (string, []map[string]any, error) {
	if s == nil || s.Engine == nil || conv == nil || strings.TrimSpace(input) == "" {
		return "", nil, nil
	}
	labels, err := s.queryLabels(ctx, conv.ID)
	if err != nil {
		return "", nil, err
	}
	// a) 向量主召回（embedding 未配置/失败 → 静默降级词法 + 日志）
	vecHits, vecErr := s.vectorRecall(ctx, conv.ID, labels, input)
	if vecErr != nil {
		log.Printf("[companion] 向量召回不可用，降级词法（会话 %s）: %v", conv.ID, vecErr)
	}
	// b) 词法兜底 + 合并去重（向量优先占坑，词法补位）
	hits, matchOf := mergeRecall(vecHits, recallEntities(labels, input))
	if len(hits) == 0 {
		return "", nil, nil
	}
	out := make([]entityHit, 0, len(hits))
	for _, label := range hits {
		h := entityHit{Label: label, Match: matchOf[label]}
		def, conf, err := s.queryEntityInfo(ctx, conv.ID, label)
		if err == nil {
			h.Definition, h.Confidence = def, conf
		}
		if edges, err := s.queryEntityNeighborhood(ctx, conv.ID, label); err == nil {
			h.Edges, h.HasEdgeInfo = edges, true
		}
		out = append(out, h)
	}
	return renderCompanionContext(out), entityDetails(out), nil
}

// vectorRecall 向量召回：标签向量（进程内缓存，图写入失效）与输入向量余弦 topK≤5 阈值 0.35。
// embedding 连接取全局默认（GetDefaultConnection，REQ-46 机制，不新建伴生专属配置）。
func (s *Service) vectorRecall(ctx context.Context, convID string, labels []string, input string) ([]string, error) {
	if len(labels) == 0 {
		return nil, nil
	}
	emb := &kb.Embedder{Store: s.Store, Box: s.Box}
	labelVecs, err := s.labelVectors(ctx, convID, labels, emb)
	if err != nil {
		return nil, err
	}
	inVec, err := emb.EmbedOne(ctx, input)
	if err != nil {
		return nil, err
	}
	type scored struct {
		label string
		sim   float64
	}
	var cands []scored
	for l, v := range labelVecs {
		if sim := cosineSim(inVec, v); sim >= vecMatchThreshold {
			cands = append(cands, scored{l, sim})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].sim > cands[j].sim })
	if len(cands) > maxRecallEntities {
		cands = cands[:maxRecallEntities]
	}
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.label)
	}
	return out, nil
}

// labelVectors 标签批量向量（缓存命中跳过；缺失标签一次批量补齐后写缓存）。
func (s *Service) labelVectors(ctx context.Context, convID string, labels []string, emb *kb.Embedder) (map[string][]float32, error) {
	s.vecMu.Lock()
	cache := s.vecCache[convID]
	if cache == nil {
		cache = map[string][]float32{}
		s.vecCache[convID] = cache
	}
	var missing []string
	for _, l := range labels {
		if _, ok := cache[l]; !ok {
			missing = append(missing, l)
		}
	}
	s.vecMu.Unlock()
	if len(missing) > 0 {
		vecs, err := emb.EmbedTexts(ctx, missing)
		if err != nil {
			return nil, err
		}
		s.vecMu.Lock()
		// 失效竞态防护：若期间缓存被图写入清空，仍以本次结果重建（标签随后会再失效）
		cur := s.vecCache[convID]
		if cur == nil {
			cur = map[string][]float32{}
			s.vecCache[convID] = cur
		}
		for i, l := range missing {
			if i < len(vecs) {
				cur[l] = vecs[i]
			}
		}
		s.vecMu.Unlock()
	}
	s.vecMu.Lock()
	defer s.vecMu.Unlock()
	out := make(map[string][]float32, len(labels))
	for _, l := range labels {
		if v, ok := s.vecCache[convID][l]; ok {
			out[l] = v
		}
	}
	return out, nil
}

// cosineSim 余弦相似度（维度不齐按短边截断；零向量返回 0）。
func cosineSim(a, b []float32) float64 {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// mergeRecall 两路命中合并去重（纯函数）：向量优先占坑，词法补位至上限；
// match 标记：仅向量=vector / 仅词法=lexical / 双中=vector+lexical。
func mergeRecall(vecHits, lexHits []string) ([]string, map[string]string) {
	matchOf := map[string]string{}
	merged := make([]string, 0, maxRecallEntities)
	seen := map[string]bool{}
	add := func(label, src string) bool {
		if seen[label] {
			if matchOf[label] != src && (matchOf[label] == "vector" || src == "vector") {
				// 双中：保留 vector 标记优先级（vector+lexical 语义等价，取并表述）
				matchOf[label] = "vector+lexical"
			}
			return false
		}
		seen[label] = true
		matchOf[label] = src
		merged = append(merged, label)
		return true
	}
	for _, l := range vecHits {
		if len(merged) >= maxRecallEntities {
			break
		}
		add(strings.TrimSpace(l), "vector")
	}
	for _, l := range lexHits {
		if len(merged) >= maxRecallEntities {
			break
		}
		add(strings.TrimSpace(l), "lexical")
	}
	return merged, matchOf
}

// queryLabels 会话伴生图实体标签清单。引擎不在位（未启动且预期端点无存活实例）时返回空——
// 读侧不拉起引擎（AdoptRunning 仅领养），空图不付出任何代价；写侧 confirm/抽取负责生命周期。
func (s *Service) queryLabels(ctx context.Context, convID string) ([]string, error) {
	if s.Engine.Endpoint() == "" && !s.Engine.AdoptRunning(ctx) {
		return nil, nil
	}
	raw, err := s.Engine.Query(ctx, SelectLabels(convID))
	if err != nil {
		return nil, err
	}
	return parseLabelValues(raw), nil
}

func (s *Service) queryEntityInfo(ctx context.Context, convID, label string) (string, float64, error) {
	raw, err := s.Engine.Query(ctx, SelectEntityInfo(convID, label))
	if err != nil {
		return "", 0, err
	}
	var res struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &res) != nil || len(res.Results.Bindings) == 0 {
		return "", 0, nil
	}
	b := res.Results.Bindings[0]
	conf := 0.0
	if v, ok := b["conf"]; ok {
		fmt.Sscanf(v.Value, "%f", &conf)
	}
	return b["def"].Value, conf, nil
}

// queryEntityNeighborhood 命中实体 2 跳邻域（1 跳优先，2 跳补位；每实体限流 ≤8 边）。
func (s *Service) queryEntityNeighborhood(ctx context.Context, convID, label string) ([]edgeRow, error) {
	raw, err := s.Engine.Query(ctx, SelectEntityNeighborhood(convID, label))
	if err != nil {
		return nil, err
	}
	var res struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &res) != nil {
		return nil, nil
	}
	edges := make([]edgeRow, 0, len(res.Results.Bindings))
	for _, b := range res.Results.Bindings {
		hop := 1
		if v, ok := b["hop"]; ok {
			fmt.Sscanf(v.Value, "%d", &hop)
		}
		if len(edges) >= maxEntityEdges {
			break
		}
		edges = append(edges, edgeRow{Rel: b["relName"].Value, Other: b["otherLabel"].Value, Dir: b["dir"].Value, Hop: hop})
	}
	return edges, nil
}

// recallEntities 标签→输入包含匹配（双向包含，短标签 <2 字符跳过防误召），保序去重限量。
func recallEntities(labels []string, input string) []string {
	lc := strings.ToLower(strings.TrimSpace(input))
	if lc == "" {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, maxRecallEntities)
	for _, l := range labels {
		t := strings.TrimSpace(l)
		if len([]rune(t)) < 2 || seen[t] {
			continue
		}
		lt := strings.ToLower(t)
		if strings.Contains(lc, lt) || strings.Contains(lt, lc) && len([]rune(lc)) >= 4 {
			seen[t] = true
			out = append(out, t)
			if len(out) >= maxRecallEntities {
				break
			}
		}
	}
	return out
}

// renderCompanionContext 检索上下文渲染（纯函数；System 注入口径对齐 kb.RenderContext）。
// REQ-194②：match 来源与 2 跳边如实标注（诚实原则——LLM 与人看到同一份事实来源）。
func renderCompanionContext(hits []entityHit) string {
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("【伴生图检索】以下内容来自本会话对话中确认生成的伴生轻量本体（供参考，非权威知识）：")
	for _, h := range hits {
		fmt.Fprintf(&b, "\n- 实体「%s」", h.Label)
		if h.Match != "" {
			fmt.Fprintf(&b, "（%s 召回）", matchLabel(h.Match))
		}
		if h.Definition != "" {
			fmt.Fprintf(&b, "：%s", h.Definition)
		}
		for _, e := range h.Edges {
			hopTag := ""
			if e.Hop == 2 {
				hopTag = "（2跳）"
			}
			if e.Dir == "out" {
				fmt.Fprintf(&b, "；「%s」→ %s%s", e.Rel, e.Other, hopTag)
			} else {
				fmt.Fprintf(&b, "；%s →「%s」%s", e.Other, e.Rel, hopTag)
			}
		}
	}
	return b.String()
}

// matchLabel match 值 → 中文标注（事件与上下文共用）。
func matchLabel(m string) string {
	switch m {
	case "vector":
		return "向量"
	case "lexical":
		return "词法"
	case "vector+lexical":
		return "向量+词法"
	default:
		return m
	}
}

// entityDetails 实体明细 → retrieval 事件数据（map 切片，chat 层透传；含 match/hop 如实呈现）。
func entityDetails(hits []entityHit) []map[string]any {
	out := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		rels := make([]map[string]any, 0, len(h.Edges))
		for _, e := range h.Edges {
			rels = append(rels, map[string]any{"rel": e.Rel, "other": e.Other, "dir": e.Dir, "hop": e.Hop})
		}
		out = append(out, map[string]any{"label": h.Label, "definition": h.Definition, "confidence": h.Confidence, "match": h.Match, "relations": rels})
	}
	return out
}

// parseLabelValues 从 SelectLabels 结果提取 label 值数组。
func parseLabelValues(raw []byte) []string {
	var res struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &res) != nil {
		return nil
	}
	out := make([]string, 0, len(res.Results.Bindings))
	for _, b := range res.Results.Bindings {
		if v, ok := b["label"]; ok {
			out = append(out, v.Value)
		}
	}
	return out
}
