// M16 阶段二（REQ-130）：社区检测（轻量 label propagation）+ 社区摘要（LLM 主路径 / 骨架回退）
// + 全局问答检索（社区摘要关键词评分）。学习口径：确定性、可单测、零外部依赖。
package kg

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// Community 检测输出（Label = 成员中字典序最小的实体名，稳定可复现）。
type Community struct {
	Label   string
	Members []string
}

// DetectCommunities label propagation 社区检测：
//   - 每实体初始 label = 自身；迭代取邻居中出现最多的 label（并列保持现状）；最多 32 轮或收敛；
//   - 仅统计 approved 关系；无关系边（或被过滤后无边）的实体各自成单例社区；
//   - 确定性：实体顺序固定（字典序）+ 并列不切换，保证同图同结果（可单测）。
func DetectCommunities(entities []string, rels [][2]string) []Community {
	names := append([]string{}, entities...)
	sort.Strings(names)
	label := map[string]string{}
	for _, n := range names {
		label[n] = n
	}
	adj := map[string][]string{}
	for _, r := range rels {
		a, b := r[0], r[1]
		if a == b {
			continue
		}
		adj[a] = append(adj[a], b)
		adj[b] = append(adj[b], a)
	}
	order := append([]string{}, names...)
	for iter := 0; iter < 32; iter++ {
		changed := false
		for _, n := range order {
			counts := map[string]int{}
			for _, m := range adj[n] {
				counts[label[m]]++
			}
			if len(counts) == 0 {
				continue
			}
			best, bestN := label[n], -1
			// 稳定并列规则：计数最高者优先，并列取字典序最小（遍历 names 已有序）
			for _, cand := range names {
				if c := counts[cand]; c > 0 {
					if c > bestN || (c == bestN && cand < best) {
						best, bestN = cand, c
					}
				}
			}
			if bestN > 0 && best != label[n] {
				label[n] = best
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	groups := map[string][]string{}
	for _, n := range names {
		groups[label[n]] = append(groups[label[n]], n)
	}
	out := []Community{}
	for _, members := range groups {
		sort.Strings(members)
		out = append(out, Community{Label: members[0], Members: members})
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Members) != len(out[j].Members) {
			return len(out[i].Members) > len(out[j].Members)
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// Summarizer 社区摘要生成（LLM 主路径，失败回退骨架；method 记录生成方式）。
type Summarizer struct {
	Store  *store.Store
	Box    *secrets.Box
	ConnID string // 库级抽取连接（REQ-129① 同源）；空 = 默认 chat
}

// communitySumSchema 摘要 JSON 契约（复用 REQ-98 能力代理）。
const communitySumSchema = `{"summary":"用 2~3 句话概括该社区整体在讲什么（面向「这个库整体在讲什么」的全局问题）"}`

// SummarizeCommunity 生成单社区摘要；LLM 失败回退骨架（成员 + 关系计数），永不 error。
func (x *Summarizer) SummarizeCommunity(ctx context.Context, label string, members, relTypes []string) (summary, method string) {
	skeleton := fmt.Sprintf("该社区以「%s」为核心，共 %d 个成员实体：%s；内部关系 %d 条（主要类型：%s）。",
		label, len(members), strings.Join(members, "、"), len(relTypes), strings.Join(relTypes, "、"))
	prompt := "以下是知识图谱中一个社区（一组紧密关联的实体）的成员与关系。请用 2~3 句话概括这个社区整体在讲什么，" +
		"回答应面向全局性问题（如「这个库整体在讲什么」）。\n成员实体：" + strings.Join(members, "、") +
		"\n内部关系类型：" + strings.Join(relTypes, "、") + "\n只输出 JSON。"
	res, err := chat.GenerateStructured(ctx, x.Store, x.Box, x.ConnID, prompt, communitySumSchema)
	if err != nil {
		return skeleton, "skeleton"
	}
	var out struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(res.DraftJSON), &out); err != nil || strings.TrimSpace(out.Summary) == "" {
		return skeleton, "skeleton"
	}
	return strings.TrimSpace(out.Summary), "llm"
}

// BuildKGCommunities 检测 + 摘要 + 持久化（重建全量替换；社区数上限 12 防摘要 token 失控，
// 超出按成员数取前 12，其余实体并入「长尾」单社区骨架摘要）。
func (x *Summarizer) BuildKGCommunities(ctx context.Context, kbID string) (int, error) {
	entities, rels, err := x.Store.KGByKB(kbID)
	if err != nil {
		return 0, err
	}
	names := make([]string, 0, len(entities))
	for _, e := range entities {
		names = append(names, e.Name)
	}
	pairs := make([][2]string, 0, len(rels))
	relTypesByPair := map[string][]string{}
	for _, r := range rels {
		pairs = append(pairs, [2]string{r.Source, r.Target})
		relTypesByPair[r.Source+"|"+r.Target] = append(relTypesByPair[r.Source+"|"+r.Target], r.Type)
	}
	comms := DetectCommunities(names, pairs)
	if len(comms) > 12 { // 长尾合并（按成员数保留前 11，其余并入尾部社区）
		keep, rest := comms[:11], comms[11:]
		members := []string{}
		for _, c := range rest {
			members = append(members, c.Members...)
		}
		sort.Strings(members)
		comms = append(keep, Community{Label: "__tail__", Members: members})
	}
	out := make([]*store.KGCommunity, 0, len(comms))
	for _, c := range comms {
		set := map[string]bool{}
		for _, m := range c.Members {
			set[m] = true
		}
		// 社区内部关系类型（两端均在社区内）
		typeCount := map[string]int{}
		seenType := []string{}
		for _, r := range rels {
			if set[r.Source] && set[r.Target] {
				if typeCount[r.Type] == 0 {
					seenType = append(seenType, r.Type)
				}
				typeCount[r.Type]++
			}
		}
		summary, method := x.SummarizeCommunity(ctx, c.Label, c.Members, seenType)
		if c.Label == "__tail__" {
			summary = "其他长尾社区：" + summary
			method += "+tail"
		}
		out = append(out, &store.KGCommunity{KBID: kbID, Label: c.Label, Summary: summary, Method: method, Members: c.Members})
	}
	if err := x.Store.ReplaceKGCommunities(kbID, out); err != nil {
		return 0, err
	}
	return len(out), nil
}

// GlobalSearchHit 全局问答命中（社区摘要检索）。
type GlobalSearchHit struct {
	Label    string   `json:"label"`
	Summary  string   `json:"summary"`
	Members  []string `json:"members"`
	Score    int      `json:"score"`
}

// GlobalSearch 全局问答（REQ-130）：查询词与社区摘要/成员做中文 2-gram 覆盖评分（零 embedding 依赖，
// 学习口径）；社区未建时提示先重建。
func GlobalSearch(kbID, query string, comms []*store.KGCommunity) []GlobalSearchHit {
	if query == "" || len(comms) == 0 {
		return []GlobalSearchHit{}
	}
	terms := gramTerms(query)
	hits := []GlobalSearchHit{}
	for _, c := range comms {
		hay := c.Summary + " " + strings.Join(c.Members, " ")
		score := 0
		for _, t := range terms {
			if strings.Contains(hay, t) {
				score++
			}
		}
		if score > 0 {
			hits = append(hits, GlobalSearchHit{Label: c.Label, Summary: c.Summary, Members: c.Members, Score: score})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	return hits
}

// gramTerms 中文 2-gram + 空格分词（零分词依赖的粗粒度匹配面）。
func gramTerms(q string) []string {
	set := map[string]bool{}
	for _, field := range strings.Fields(q) {
		rs := []rune(field)
		if len(rs) <= 2 {
			set[field] = true
			continue
		}
		for i := 0; i+2 <= len(rs); i++ {
			set[string(rs[i:i+2])] = true
		}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}
