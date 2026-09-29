package companion

import (
	"fmt"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// ---------------------------------------------------------------------------
// REQ-194/M34 批次一①：抽取时实体对齐（治 G2 无对齐归并 / G3 自动入图放大图污染）。
// 抽取前把会话伴生图已有实体清单注入 prompt（涉已有实体必须沿用原名），抽取产物
// 打 aligned 标记（aligned=沿用已有实体 / new=新造）落候选表，管理界面徽标呈现。
// 纯函数集中于本文件，便于零依赖单测。
// ---------------------------------------------------------------------------

// alignmentLabelCap 清单注入上限（>80 按 slug 前缀聚类取样截断，防 prompt 膨胀）。
const alignmentLabelCap = 80

// buildAlignmentSection 已有实体清单 → prompt 约束节（空清单返回空串=走原行为，不新增依赖）。
func buildAlignmentSection(labels []string) string {
	clean := make([]string, 0, len(labels))
	seen := map[string]bool{}
	for _, l := range labels {
		t := strings.TrimSpace(l)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		clean = append(clean, t)
	}
	if len(clean) == 0 {
		return ""
	}
	shown := clean
	note := ""
	if len(clean) > alignmentLabelCap {
		shown = clusterSampleBySlug(clean, alignmentLabelCap)
		note = fmt.Sprintf("（清单共 %d 条，已按前缀聚类取样展示 %d 条——未展示实体同样适用沿用原名规则）", len(clean), len(shown))
	}
	var b strings.Builder
	b.WriteString("\n本会话伴生图已有实体清单" + note + "：[" + strings.Join(shown, "、") + "]\n")
	b.WriteString("抽取规则（对齐约束，优先级高于新造命名）：\n")
	b.WriteString("a. 新事实涉及清单中的实体时，必须沿用清单原名（含后缀与写法），不要另造同义/近似名称。\n")
	b.WriteString("b. 清单实体被进一步刻画（新增关系、定义修正）时同样沿用原名。\n")
	b.WriteString("c. 仅确属新实体才新造名，且新造名不得与清单实体同义近似。\n")
	return b.String()
}

// slugPrefixKey 聚类键：前 2 个 CJK 字符或前 8 个拉丁/数字字符（先到为准；混合取先触达者）。
func slugPrefixKey(label string) string {
	var b strings.Builder
	latin, cjk := 0, 0
	for _, r := range strings.TrimSpace(label) {
		switch {
		case r >= 0x4e00 && r <= 0x9fff:
			if cjk >= 2 {
				return b.String()
			}
			cjk++
			b.WriteRune(r)
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			if latin >= 8 {
				return b.String()
			}
			latin++
			b.WriteRune(r)
		default:
			// 分隔符作为簇内边界信号：已有内容即截断
			if b.Len() > 0 {
				return b.String()
			}
		}
	}
	return b.String()
}

// clusterSampleBySlug 前缀聚类取样（确定性轮转：各簇轮流取一条直到 limit，保多主题覆盖）。
func clusterSampleBySlug(labels []string, limit int) []string {
	clusters := map[string][]string{}
	var order []string // 簇出现序（确定性）
	for _, l := range labels {
		k := slugPrefixKey(l)
		if _, ok := clusters[k]; !ok {
			order = append(order, k)
		}
		clusters[k] = append(clusters[k], l)
	}
	idx := map[string]int{}
	out := make([]string, 0, limit)
	for len(out) < limit {
		progressed := false
		for _, k := range order {
			list := clusters[k]
			if i, ok := idx[k]; ok && i >= len(list) {
				continue
			}
			i := idx[k]
			out = append(out, list[i])
			idx[k] = i + 1
			progressed = true
			if len(out) >= limit {
				break
			}
		}
		if !progressed {
			break
		}
	}
	return out
}

// markAligned 候选对齐标记（REQ-194①）：concept/event 看 name；relation 看 source 或 target
// 任一命中已有清单即为 aligned（沿用已有实体的关系陈述），否则 new。
func markAligned(cands []*store.CompanionCandidate, known []string) {
	set := map[string]bool{}
	for _, k := range known {
		set[Slug(strings.TrimSpace(k))] = true
	}
	for _, c := range cands {
		aligned := set[Slug(c.Name)]
		if !aligned && c.Kind == "relation" {
			aligned = set[Slug(c.RelTarget)]
		}
		if aligned {
			c.Aligned = "aligned"
		} else {
			c.Aligned = "new"
		}
	}
}

// appendWindowEntities 分窗抽取跨窗对齐：上一窗产物涉及的实体名并入下窗清单（同名 slug 去重）。
func appendWindowEntities(known []string, cands []*store.CompanionCandidate) []string {
	set := map[string]bool{}
	for _, k := range known {
		set[Slug(strings.TrimSpace(k))] = true
	}
	add := func(n string) {
		n = strings.TrimSpace(n)
		if n == "" {
			return
		}
		if !set[Slug(n)] {
			set[Slug(n)] = true
			known = append(known, n)
		}
	}
	for _, c := range cands {
		add(c.Name)
		if c.Kind == "relation" {
			add(c.RelTarget)
		}
	}
	return known
}

// ---------------------------------------------------------------------------
// REQ-194/M34 批次二⑤：语义矛盾检测（治 G6 矛盾检测仅同主体+同关系名单跳）。
// 同关系名不同目标 = 确定性矛盾（既有路径，无 LLM）；不同关系名的语义冲突交 LLM 二分类。
// ---------------------------------------------------------------------------

// conflictSchema 语义矛盾二分类输出契约（yes=冲突失效化 / unsure=双保留+note 待人工）。
const conflictSchema = `{
  "type": "object",
  "properties": {
    "conflict": {"type": "string", "enum": ["yes", "no", "unsure"]},
    "conflict_rel_name": {"type": "string"},
    "conflict_object": {"type": "string"},
    "reason": {"type": "string"}
  },
  "required": ["conflict"]
}`

type conflictOut struct {
	Conflict        string `json:"conflict"`
	ConflictRelName string `json:"conflict_rel_name"`
	ConflictObject  string `json:"conflict_object"`
	Reason          string `json:"reason"`
}

// edgeAssertion 同主体一条活跃断言（矛盾检测数据行）。
type edgeAssertion struct {
	Edge     string // 边节点 URI（失效化定位用）
	RelName  string
	ObjLabel string
}

// conflictPrompt 语义矛盾二分类提示词（纯函数；只判互斥，细化/补充不算冲突——宁保留勿误杀）。
func conflictPrompt(subject string, existing []edgeAssertion, newRel, newTarget string) string {
	var b strings.Builder
	b.WriteString("你是知识图谱一致性审查助手。判断「新断言」与同一主体的既有断言是否存在语义矛盾（两者不能同时为真）。\n")
	b.WriteString("判定标准：\n")
	b.WriteString("- 冲突：语义互斥（如「是良性的」vs「是恶性的」、「已下线」vs「仍在运行」）。\n")
	b.WriteString("- 不冲突：同一事实的不同表述、补充细节、属性细化、并列成立的信息。\n")
	b.WriteString("- 无法确定时如实输出 unsure，不要猜测。\n\n")
	fmt.Fprintf(&b, "主体：%s\n既有断言：\n", subject)
	for i, e := range existing {
		fmt.Fprintf(&b, "[%d] %s —%s→ %s\n", i+1, subject, e.RelName, e.ObjLabel)
	}
	fmt.Fprintf(&b, "新断言：%s —%s→ %s\n\n", subject, newRel, newTarget)
	b.WriteString("若冲突，conflict_rel_name/conflict_object 填被冲突既有断言的关系名与客体（按既有断言原文）；reason 一句话说明。只输出 JSON。")
	return b.String()
}

// matchConflictEdge 按 LLM 指认（关系名+客体，宽松包含匹配）定位被冲突旧边；指认不中回退同关系名首条。
func matchConflictEdge(existing []edgeAssertion, relName, objLabel string) string {
	rel, obj := strings.TrimSpace(relName), strings.TrimSpace(objLabel)
	if rel != "" && obj != "" {
		for _, e := range existing {
			if e.RelName == rel && (e.ObjLabel == obj || strings.Contains(e.ObjLabel, obj) || strings.Contains(obj, e.ObjLabel)) {
				return e.Edge
			}
		}
	}
	if rel != "" {
		for _, e := range existing {
			if e.RelName == rel {
				return e.Edge
			}
		}
	}
	if len(existing) > 0 {
		return existing[0].Edge
	}
	return ""
}
