package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// ---------------------------------------------------------------------------
// REQ-170 P2「KG 检索源并入」（REQ-127~130 检索源矩阵 × 伴生图）：对话检索期把会话伴生图
// 作为一路检索源——实体标签与本次输入做包含匹配，命中实体拉取定义与活跃关系边，
// 渲染为检索上下文（chat 层以 System 消息注入）+ retrieval 事件明细（source=companion）。
// 与 KB 召回同口径：引擎不可用/无命中返回空上下文（不视为错误），失败降级不阻断主链路。
// ---------------------------------------------------------------------------

// maxRecallEntities 单次并入检索的命中实体上限（薄版口径：少而准，避免上下文淹没）。
const maxRecallEntities = 5

// edgeRow 一条活跃关系边的渲染行。
type edgeRow struct {
	Rel   string `json:"rel"`
	Other string `json:"other"`
	Dir   string `json:"dir"` // out=主体→他者 | in=他者→主体
}

// entityHit 命中实体明细（retrieval 事件与上下文渲染共用）。
type entityHit struct {
	Label       string    `json:"label"`
	Definition  string    `json:"definition,omitempty"`
	Confidence  float64   `json:"confidence,omitempty"`
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
	hits := recallEntities(labels, input)
	if len(hits) == 0 {
		return "", nil, nil
	}
	out := make([]entityHit, 0, len(hits))
	for _, label := range hits {
		h := entityHit{Label: label}
		def, conf, err := s.queryEntityInfo(ctx, conv.ID, label)
		if err == nil {
			h.Definition, h.Confidence = def, conf
		}
		if edges, err := s.queryEntityEdges(ctx, conv.ID, label); err == nil {
			h.Edges, h.HasEdgeInfo = edges, true
		}
		out = append(out, h)
	}
	return renderCompanionContext(out), entityDetails(out), nil
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

func (s *Service) queryEntityEdges(ctx context.Context, convID, label string) ([]edgeRow, error) {
	raw, err := s.Engine.Query(ctx, SelectEntityEdges(convID, label))
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
		edges = append(edges, edgeRow{Rel: b["relName"].Value, Other: b["otherLabel"].Value, Dir: b["dir"].Value})
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
func renderCompanionContext(hits []entityHit) string {
	if len(hits) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("【伴生图检索】以下内容来自本会话对话中确认生成的伴生轻量本体（供参考，非权威知识）：")
	for _, h := range hits {
		fmt.Fprintf(&b, "\n- 实体「%s」", h.Label)
		if h.Definition != "" {
			fmt.Fprintf(&b, "：%s", h.Definition)
		}
		for _, e := range h.Edges {
			if e.Dir == "out" {
				fmt.Fprintf(&b, "；「%s」→ %s", e.Rel, e.Other)
			} else {
				fmt.Fprintf(&b, "；%s →「%s」", e.Other, e.Rel)
			}
		}
	}
	return b.String()
}

// entityDetails 实体明细 → retrieval 事件数据（map 切片，chat 层透传）。
func entityDetails(hits []entityHit) []map[string]any {
	out := make([]map[string]any, 0, len(hits))
	for _, h := range hits {
		rels := make([]map[string]any, 0, len(h.Edges))
		for _, e := range h.Edges {
			rels = append(rels, map[string]any{"rel": e.Rel, "other": e.Other, "dir": e.Dir})
		}
		out = append(out, map[string]any{"label": h.Label, "definition": h.Definition, "confidence": h.Confidence, "relations": rels})
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
