// KB-5（M35/37 号方案）：全局问答闭环——社区摘要按需生成+缓存（D5 采纳）+ 社区摘要 map-reduce
// 进 LLM 上下文生成回答（对标微软 GraphRAG global search 语义）+ chat.CommunitySource 适配器
//（对话兜底接入，SC-K7；接口在 chat 侧定义，kg 已依赖 chat 不成环）。
package kg

import (
	"context"
	"fmt"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

const (
	globalTopN    = 4 // 端点 map-reduce 进入生成环节的最大社区数（token 硬顶）
	communityTopN = 3 // 对话兜底注入的最大社区数
)

// ensureSummary 确保单社区有摘要（缺失则生成并持久化 = 缓存；LLM 失败回退骨架也落库，method 如实）。
func (x *Summarizer) ensureSummary(ctx context.Context, kbID, connID string, c *store.KGCommunity, rels []*store.KGRelationship) {
	if c.Summary != "" {
		return
	}
	if connID == "" {
		connID = x.ConnID
	}
	summary, method := x.SummarizeCommunity(ctx, c.Label, c.Members, internalRelTypes(rels, c.Members))
	if c.Label == "__tail__" {
		summary = "其他长尾社区：" + summary
		method += "+tail"
	}
	if err := x.Store.UpdateKGCommunitySummary(kbID, c.Label, summary, method); err == nil {
		c.Summary, c.Method = summary, method
	}
}

// topCommunities 2-gram 评分取 TopN 相关社区（复用 GlobalSearch 评分面；摘要缺失时按成员名匹配仍可命中）。
func topCommunities(query string, comms []*store.KGCommunity, n int) []GlobalSearchHit {
	hits := GlobalSearch("", query, comms)
	if len(hits) > n {
		hits = hits[:n]
	}
	return hits
}

// GlobalAnswer 全局问答（KB-5②）：TopN 相关社区摘要 map-reduce 进 LLM 上下文生成回答。
// 摘要缺失先按需生成（D5 缓存）；返回 (回答, 命中[含生成后摘要与 used 标注], ok)。
// ok=false = 无命中或生成失败（调用方降级为摘要回显口径）。
func (x *Summarizer) GlobalAnswer(ctx context.Context, kbID, connID, query string, comms []*store.KGCommunity) (string, []GlobalSearchHit, bool) {
	if strings.TrimSpace(query) == "" || len(comms) == 0 {
		return "", nil, false
	}
	top := topCommunities(query, comms, globalTopN)
	if len(top) == 0 {
		return "", nil, false
	}
	if connID == "" {
		connID = x.ConnID
	}
	_, rels, rerr := x.Store.KGByKB(kbID)
	var relList []*store.KGRelationship
	if rerr == nil {
		relList = rels
	}
	byLabel := map[string]*store.KGCommunity{}
	for _, c := range comms {
		byLabel[c.Label] = c
	}
	parts := make([]string, 0, len(top))
	out := make([]GlobalSearchHit, 0, len(top))
	for _, h := range top {
		c := byLabel[h.Label]
		if c == nil {
			continue
		}
		x.ensureSummary(ctx, kbID, connID, c, relList)
		h.Used = true
		h.Summary = c.Summary
		out = append(out, h)
		parts = append(parts, fmt.Sprintf("[%d] 社区「%s」（成员：%s）\n%s", len(parts)+1, c.Label, strings.Join(c.Members, "、"), c.Summary))
	}
	if len(parts) == 0 {
		return "", out, false
	}
	system := "你是知识库问答助手。请仅依据给出的社区摘要回答全局性问题；摘要未覆盖的内容明确说明「知识库中未覆盖」，不要编造。"
	user := "以下是知识图谱社区摘要（每个社区是一组紧密关联的实体及其关系概要）：\n\n" +
		strings.Join(parts, "\n\n") + "\n\n问题：" + query
	answer, gerr := chat.GenerateText(ctx, x.Store, x.Box, connID, system, user, nil)
	if gerr != nil || strings.TrimSpace(answer) == "" {
		return "", out, false
	}
	return strings.TrimSpace(answer), out, true
}

// CommunityContext 实现 chat.CommunitySource（KB-5③ 对话兜底接入）。
// 按 query 匹配 TopN 社区、摘要按需生成，拼接注入上下文；ok=false = 无社区/无命中（chat 层静默跳过，
// 本地检索正常命中时不会被调用——仅 graphrag 库本地全空时兜底）。
func (x *Summarizer) CommunityContext(ctx context.Context, kbID, connID, query string) (string, []map[string]any, bool) {
	comms, err := x.Store.ListKGCommunities(kbID)
	if err != nil || len(comms) == 0 {
		return "", nil, false
	}
	top := topCommunities(query, comms, communityTopN)
	if len(top) == 0 {
		return "", nil, false
	}
	if connID == "" {
		connID = x.ConnID
	}
	_, rels, rerr := x.Store.KGByKB(kbID)
	var relList []*store.KGRelationship
	if rerr == nil {
		relList = rels
	}
	byLabel := map[string]*store.KGCommunity{}
	for _, c := range comms {
		byLabel[c.Label] = c
	}
	var b strings.Builder
	b.WriteString("# 知识库全局参考（社区摘要）\n以下是与问题最相关的知识图谱社区摘要，可用于回答全局性/概述性问题；摘要未覆盖的内容请如实说明，不要编造：\n")
	hitMaps := make([]map[string]any, 0, len(top))
	i := 0
	for _, h := range top {
		c := byLabel[h.Label]
		if c == nil {
			continue
		}
		x.ensureSummary(ctx, kbID, connID, c, relList)
		i++
		fmt.Fprintf(&b, "\n[社区 %d] %s（%d 个成员）\n%s\n", i, c.Label, len(c.Members), c.Summary)
		hitMaps = append(hitMaps, map[string]any{"label": c.Label, "score": h.Score, "members": len(c.Members)})
	}
	if i == 0 {
		return "", nil, false
	}
	return b.String(), hitMaps, true
}
