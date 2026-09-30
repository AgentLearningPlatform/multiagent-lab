// KB-6 抽取管线升级（M36/37 号正式方案；35 号 KB-6 详设）：
//
//	① 语料预算库级可配（kg_max_chunks，默认 200 解除原 60 chunks/24k 硬截断）+ 成本预估提示；
//	② 分窗并发：语料切窗（≤4 chunks 且 ≤3000 字符先到为准），worker pool 并发调 LLM；
//	③ 两步抽取（KGGen 方法论）：第一步全窗抽实体清单 → 归并出全局实体清单 →
//	   第二步按窗抽关系/claims 并约束只引用清单实体名（指代一致），未命中清单的关系如实丢弃计数；
//	④ 本体约束模式（I3 裁定：挂载点=库级抽取配置 kg_ontology_id，本体侧只读词表注入 prompt 白名单，
//	   运行期单向 本体→KB，不反向依赖）——通用 GraphRAG 框架没有的差异化能力。
package kg

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// OntoVocab 本体约束词表（只读投影：概念标签/名 + 关系名/标签）。
type OntoVocab struct {
	Name      string   `json:"name,omitempty"`
	Concepts  []string `json:"concepts"`
	Relations []string `json:"relations"`
}

// OntoVocabFunc 本体词表读取注入签名（api/main 层装配——经构建平面 GET /api/ontologies/{id}/spec；
// kg 包不 import 本体代理，「运行期单向 本体→KB」的依赖方向由装配层决定，P3 原则）。
type OntoVocabFunc func(ctx context.Context, ontologyID string) (*OntoVocab, error)

const (
	// defaultKGMaxChunks 默认语料预算（解除原 60 硬截断；库级 kg_max_chunks>0 覆盖）
	defaultKGMaxChunks = 200
	// 窗口尺寸：每窗 ≤4 chunks 且 ≤3000 字符先到为准（单窗 prompt 规模可控，抽取粒度更细）
	windowMaxChunks = 4
	windowMaxChars  = 3000
	// entityListLimit 第二步注入的全局实体名上限（防 prompt 膨胀；按出现频次截断）
	entityListLimit = 150
	// maxKGChars 全局字符兜底预算（窗口化后正常不触达；超长单 chunk 防失控）
	maxKGChars = 48000
)

// kgConcurrency 并发度（env KG_EXTRACT_CONCURRENCY，默认 3，clamp 1~8）。
func kgConcurrency() int {
	n, _ := strconv.Atoi(os.Getenv("KG_EXTRACT_CONCURRENCY"))
	if n < 1 {
		n = 3
	}
	if n > 8 {
		n = 8
	}
	return n
}

// budgetFor 库级预算（KB 行 kg_max_chunks>0 覆盖默认）。
func budgetFor(k *store.KnowledgeBase) int {
	if k != nil && k.KGMaxChunks > 0 {
		return k.KGMaxChunks
	}
	return defaultKGMaxChunks
}

type extractWindow struct {
	idx  int
	text string
}

// buildWindows chunk 池 → 抽取窗口（每窗 ≤4 chunks 且 ≤3000 字符先到为准；超预算截断）。
// 纯函数（单测覆盖）：预算与窗口数决定 LLM 调用次数（成本预估口径）。
func buildWindows(chunks []*store.KnowledgeChunk, maxChunks int) ([]extractWindow, bool) {
	if maxChunks < 1 {
		maxChunks = defaultKGMaxChunks
	}
	wins := []extractWindow{}
	var b strings.Builder
	count, winChunks := 0, 0
	truncated := false
	flush := func() {
		if b.Len() > 0 {
			wins = append(wins, extractWindow{idx: len(wins), text: b.String()})
			b.Reset()
			winChunks = 0
		}
	}
	for _, c := range chunks {
		if count >= maxChunks {
			truncated = true
			break
		}
		item := fmt.Sprintf("[doc:%s #%d] %s\n", c.DocID, c.Seq, strings.TrimSpace(c.Content))
		if winChunks >= windowMaxChunks || b.Len()+len(item) > windowMaxChars {
			flush()
		}
		b.WriteString(item)
		winChunks++
		count++
		if b.Len() >= maxKGChars {
			truncated = true
			break
		}
	}
	flush()
	return wins, truncated
}

// pass1Schema 第一步输出契约：仅实体清单。
const pass1Schema = `{"entities":[{"name":"实体名","type":"concept|individual|event|property","description":"一句话描述"}]}`

// pass2Schema 第二步输出契约：关系 + claims（source/target/subject 只能引用已确认实体清单）。
const pass2Schema = `{"relations":[{"source":"实体A","target":"实体B","type":"IS_A|具有|引发|…短语"}],"claims":[{"subject":"实体名","text":"该实体的关键事实陈述（紧贴原文）"}]}`

// vocabPrompt 本体约束词表段（KB-6③：白名单注入，超长截断防 prompt 膨胀）。
func vocabPrompt(v *OntoVocab) string {
	if v == nil || (len(v.Concepts) == 0 && len(v.Relations) == 0) {
		return ""
	}
	concepts := v.Concepts
	if len(concepts) > 120 {
		concepts = concepts[:120]
	}
	rels := v.Relations
	if len(rels) > 60 {
		rels = rels[:60]
	}
	var b strings.Builder
	b.WriteString("\n【本体词表约束（挂载本体：" + v.Name + "）】实体名优先取自以下概念词表，关系 type 优先取自关系词表；\n确有依据的新词可补充，但不得与词表语义重复另造同义词：\n")
	if len(concepts) > 0 {
		b.WriteString("概念词表：" + strings.Join(concepts, "、") + "\n")
	}
	if len(rels) > 0 {
		b.WriteString("关系词表：" + strings.Join(rels, "、") + "\n")
	}
	return b.String()
}

// mergeKey 实体归一键（trim + 压缩全部空白，大小写不敏感）。
func mergeKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(name)), ""))
}

// entityAcc 实体归并累加器（跨窗同名去重；描述/类型取首个非空；count 供频次序输出）。
type entityAcc struct {
	order []string
	byKey map[string]*kgEntityT
	count map[string]int
}

func newEntityAcc() *entityAcc {
	return &entityAcc{byKey: map[string]*kgEntityT{}, count: map[string]int{}}
}

func (a *entityAcc) add(name, typ, desc string) {
	key := mergeKey(name)
	if key == "" {
		return
	}
	a.count[key]++
	if e, ok := a.byKey[key]; ok {
		if e.Description == "" && desc != "" {
			e.Description = desc
		}
		if e.Type == "" && typ != "" {
			e.Type = typ
		}
		return
	}
	a.order = append(a.order, key)
	a.byKey[key] = &kgEntityT{Name: strings.TrimSpace(name), Type: typ, Description: desc}
}

// names 按出现频次降序的实体名清单（第二步 prompt 注入与输出序，上限 entityListLimit）。
func (a *entityAcc) names() []string {
	type pair struct {
		key   string
		count int
	}
	ps := make([]pair, 0, len(a.order))
	for _, k := range a.order {
		ps = append(ps, pair{k, a.count[k]})
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].count > ps[j].count })
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, a.byKey[p.key].Name)
		if len(out) >= entityListLimit {
			break
		}
	}
	return out
}

// llmExtractTwoPass 两步抽取主路径：返回归并后的抽取结果与过程警告。
// 全部第一步窗口失败 → 返回 error（上层回退规则抽取）；部分窗口失败 → 警告续行。
func (x *Extractor) llmExtractTwoPass(ctx context.Context, kbID string, wins []extractWindow, connID, promptOverride string, vocab *OntoVocab) (*kgOut, []string, error) {
	warnings := []string{}
	conc := kgConcurrency()
	if len(wins) == 0 {
		return nil, warnings, fmt.Errorf("无抽取窗口")
	}
	vp := vocabPrompt(vocab)

	// ---- 第一步：分窗抽实体清单 ----
	ents := newEntityAcc()
	fail1 := 0
	var mu sync.Mutex
	runPool(len(wins), conc, func(i int) {
		prompt := "你是知识工程师。以下是知识库「" + kbID + "」语料的一个片段窗口，请抽取该窗口中的核心实体清单：\n" +
			"1. entities：领域概念、个体、事件、属性；name 用唯一中文短语，type 取 concept|individual|event|property 之一，description 一句话。\n" +
			"只输出 JSON，不要输出其他内容。" + vp
		if promptOverride != "" { // M16/REQ-129①：库级提示词覆写（领域约束追加，JSON 契约保留）
			prompt = promptOverride + "\n\n【输出 JSON 契约不变，本步只抽实体清单】" + prompt
		}
		prompt += "\n窗口语料：\n" + wins[i].text
		res, err := chat.GenerateStructured(ctx, x.Store, x.Box, connID, prompt, pass1Schema)
		if err != nil {
			mu.Lock()
			fail1++
			mu.Unlock()
			log.Printf("[kg] kb=%s 窗口%d 实体抽取失败: %v", kbID, i, err)
			return
		}
		var step1 struct {
			Entities []kgEntityT `json:"entities"`
		}
		if err := json.Unmarshal(res.DraftJSON, &step1); err != nil {
			mu.Lock()
			fail1++
			mu.Unlock()
			log.Printf("[kg] kb=%s 窗口%d 实体解析失败: %v", kbID, i, err)
			return
		}
		winAcc := newEntityAcc()
		for _, e := range step1.Entities {
			winAcc.add(e.Name, e.Type, e.Description)
		}
		mu.Lock()
		for _, k := range winAcc.order {
			e := winAcc.byKey[k]
			ents.add(e.Name, e.Type, e.Description)
		}
		mu.Unlock()
	})
	if len(ents.order) == 0 {
		return nil, warnings, fmt.Errorf("全部 %d 窗实体抽取失败", len(wins))
	}
	if fail1 > 0 {
		warnings = append(warnings, fmt.Sprintf("%d/%d 窗实体抽取失败（已跳过）", fail1, len(wins)))
	}

	// ---- 第二步：注入全局实体清单，分窗抽关系/claims ----
	entityNames := ents.names()
	globalKeys := map[string]bool{}
	for _, n := range entityNames {
		globalKeys[mergeKey(n)] = true
	}
	type step2Out struct {
		rels   []kgRelT
		claims []kgClaimT
	}
	results := make([]*step2Out, len(wins))
	fail2 := 0
	runPool(len(wins), conc, func(i int) {
		prompt := "你是知识工程师。以下是知识库「" + kbID + "」语料的一个片段窗口与全局已确认实体清单，请抽取关系与事实陈述：\n" +
			"1. relations：实体间有意义的关联；source/target 只能引用下方清单中的实体名（跨窗口引用允许）；type 用短语（层级用 IS_A，整体-部分/属性用「具有」，其余用动名词如「引发」「适用于」）。\n" +
			"2. claims：每个关键实体 1~3 条事实陈述（紧贴原文的短句），subject 引用清单中的实体名。\n" +
			"只输出 JSON，不要输出其他内容。" + vp
		if promptOverride != "" {
			prompt = promptOverride + "\n\n【输出 JSON 契约不变，本步只抽关系与 claims】" + prompt
		}
		prompt += "\n已确认实体清单：" + strings.Join(entityNames, "、") + "\n窗口语料：\n" + wins[i].text
		res, err := chat.GenerateStructured(ctx, x.Store, x.Box, connID, prompt, pass2Schema)
		if err != nil {
			mu.Lock()
			fail2++
			mu.Unlock()
			log.Printf("[kg] kb=%s 窗口%d 关系抽取失败: %v", kbID, i, err)
			return
		}
		var step2 struct {
			Relations []kgRelT   `json:"relations"`
			Claims    []kgClaimT `json:"claims"`
		}
		if err := json.Unmarshal(res.DraftJSON, &step2); err != nil {
			mu.Lock()
			fail2++
			mu.Unlock()
			log.Printf("[kg] kb=%s 窗口%d 关系解析失败: %v", kbID, i, err)
			return
		}
		results[i] = &step2Out{rels: step2.Relations, claims: step2.Claims}
	})

	// ---- 归并：关系端点必须命中实体清单（归一键比对），未命中如实丢弃计数 ----
	out := &kgOut{}
	relSeen := map[string]bool{}
	dropped := 0
	claimSeen := map[string]bool{}
	for _, r := range results {
		if r == nil {
			continue
		}
		for _, rel := range r.rels {
			sk, tk := mergeKey(rel.Source), mergeKey(rel.Target)
			if !globalKeys[sk] || !globalKeys[tk] || sk == tk {
				dropped++
				continue
			}
			key := rel.Type + "|" + sk + "|" + tk
			if relSeen[key] {
				continue
			}
			relSeen[key] = true
			out.Relations = append(out.Relations, rel)
		}
		for _, c := range r.claims {
			if !globalKeys[mergeKey(c.Subject)] {
				dropped++
				continue
			}
			key := c.Subject + "|" + c.Text
			if claimSeen[key] {
				continue
			}
			claimSeen[key] = true
			out.Claims = append(out.Claims, c)
		}
	}
	if dropped > 0 {
		warnings = append(warnings, fmt.Sprintf("%d 条关系/claims 因引用未确认实体被丢弃（两步抽取指代一致性约束）", dropped))
	}
	if fail2 > 0 {
		warnings = append(warnings, fmt.Sprintf("%d/%d 窗关系抽取失败（已跳过）", fail2, len(wins)))
	}
	for _, n := range entityNames { // 实体按频次序输出
		out.Entities = append(out.Entities, *ents.byKey[mergeKey(n)])
	}
	return out, warnings, nil
}

// runPool 定长 worker pool（零新依赖：semaphore channel + WaitGroup）。
func runPool(n, conc int, fn func(i int)) {
	if conc < 1 {
		conc = 1
	}
	sem := make(chan struct{}, conc)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i)
		}(i)
	}
	wg.Wait()
}
