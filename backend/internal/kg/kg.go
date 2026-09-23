// Package kg 自研轻量 KG 抽取（D-O15/REQ-110：去-semantica 化，反转 D-O10）。
//
// 抽取切 REQ-98 LLM 能力代理（chat.GenerateStructured，主路径 method=llm）；
// LLM 不可用/解析失败时回退内置规则抽取（「A 是 B」「A 的 B」句式，method=lightweight），
// 保证 graphrag 模式零外部进程依赖（验收 22：run-dev.sh 一条命令、零 Python venv）。
// 独立成包原因：chat → kb 已有依赖，抽取若放 kb 包会成环；由 api 层注入 kb.Service。
package kg

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// 预算（对齐 ontobuild 策略 A 口径的收紧版：KG 抽取粒度小、条目多，语料预算减半防 token 失控）
const (
	maxKGChunks = 60
	maxKGChars  = 24000
)

// Extractor KG 抽取器（api 层构造并注入 kb.Service.KGExtract）。
type Extractor struct {
	Store  *store.Store
	Box    *secrets.Box
	ConnID string // 兜底模型连接；空 = 默认 chat 连接（REQ-98 规则）
}

// Extractor 抽取输出（LLM JSON 契约；轻量回退同形构造）。
type kgOut struct {
	Entities []struct {
		Name        string `json:"name"`
		Type        string `json:"type"`
		Description string `json:"description"`
	} `json:"entities"`
	Relations []struct {
		Source string `json:"source"`
		Target string `json:"target"`
		Type   string `json:"type"`
	} `json:"relations"`
	Claims []struct {
		Subject string `json:"subject"`
		Text    string `json:"text"`
	} `json:"claims"`
}

const kgSchema = `{"entities":[{"name":"实体名","type":"concept|individual|event|property","description":"一句话描述"}],"relations":[{"source":"实体A","target":"实体B","type":"IS_A|具有|引发|…短语"}],"claims":[{"subject":"实体名","text":"该实体的关键事实陈述（紧贴原文）"}]}`

// ExtractForDoc 抽取某文档（或 KB 级重建 docID=""）的 KG 并落库。
// 永不返回 error：结果语义对齐 M14 GraphragInfo（degraded 不阻断导入主流程）。
func (x *Extractor) ExtractForDoc(ctx context.Context, kbID, docID string, chunks []*store.KnowledgeChunk) *store.GraphragInfo {
	if len(chunks) == 0 {
		return &store.GraphragInfo{Degraded: true, Error: "无 chunk 语料，KG 抽取跳过"}
	}
	corpus, truncated := buildCorpus(chunks, maxKGChunks, maxKGChars)
	warnings := []string{}
	connID, promptOverride := x.kbConfig(kbID) // M16/REQ-129①：库级抽取配置
	out, llmErr := x.llmExtract(ctx, kbID, corpus, connID, promptOverride)
	method := "llm"
	if llmErr != nil {
		method = "lightweight"
		out = ruleExtract(chunks)
		warnings = append(warnings, "LLM 抽取失败，已回退规则抽取（「A 是 B」「A 的 B」句式）: "+llmErr.Error())
		log.Printf("[kg] kb=%s doc=%s LLM 抽取失败回退 lightweight: %v", kbID, docID, llmErr)
	}
	if truncated {
		warnings = append(warnings, fmt.Sprintf("语料超预算被截断（上限 %d chunks / %d chars），KG 覆盖不完整", maxKGChunks, maxKGChars))
	}
	if len(out.Entities) == 0 && len(out.Relations) == 0 && len(out.Claims) == 0 {
		return &store.GraphragInfo{Degraded: true, Method: method, Chunks: len(chunks), Warnings: warnings, Error: "未抽取到任何实体/关系（语料过短或句式不匹配）"}
	}
	if err := x.persist(kbID, docID, out, chunks); err != nil {
		return &store.GraphragInfo{Degraded: true, Method: method, Chunks: len(chunks), Error: "KG 落库失败: " + err.Error(), Warnings: warnings}
	}
	info := &store.GraphragInfo{OK: true, Method: method, Chunks: len(chunks),
		Entities: len(out.Entities), Relationships: len(out.Relations), Warnings: warnings}
	log.Printf("[kg] kb=%s doc=%s 抽取完成: method=%s entities=%d relations=%d claims=%d",
		kbID, docID, method, len(out.Entities), len(out.Relations), len(out.Claims))
	return info
}

// llmExtract REQ-98 能力代理抽取（解析失败/模型不可用返回 error，由上层回退）。
func (x *Extractor) llmExtract(ctx context.Context, kbID, corpus, connID, promptOverride string) (*kgOut, error) {
	prompt := "你是知识工程师。以下是知识库「" + kbID + "」的 chunk 语料。请通读后抽取知识图谱（KG）：\n" +
		"1. entities：语料中的核心实体（领域概念、个体、事件、属性），name 用唯一中文短语，type 取 concept|individual|event|property 之一，description 一句话。\n" +
		"2. relations：实体间有意义的关联，source/target 必须引用已有 entity name，type 用短语（层级用 IS_A，整体-部分/属性用「具有」，其余用动名词如「引发」「适用于」）。\n" +
		"3. claims：每个关键实体 1~3 条事实陈述（紧贴原文的短句），subject 引用 entity name。\n" +
		"只输出 JSON，不要输出其他内容。语料：\n" + corpus
	if promptOverride != "" { // M16/REQ-129①：库级提示词覆写（追加领域约束，JSON 契约与语料段保留）
		prompt = promptOverride + "\n\n【输出 JSON 契约不变】" + prompt
	}
	res, err := chat.GenerateStructured(ctx, x.Store, x.Box, connID, prompt, kgSchema)
	if err != nil {
		return nil, err
	}
	var out kgOut
	if err := json.Unmarshal([]byte(res.DraftJSON), &out); err != nil {
		return nil, fmt.Errorf("模型输出解析失败: %w", err)
	}
	normalize(&out)
	return &out, nil
}

// kbConfig 解析库级抽取配置（REQ-129①）：模型连接与提示词覆写（KB 配置优先，均空回退默认）。
func (x *Extractor) kbConfig(kbID string) (connID, promptOverride string) {
	connID = x.ConnID
	if x.Store != nil {
		if k, err := x.Store.GetKnowledgeBase(kbID); err == nil && k != nil {
			if k.KGConnID != "" {
				connID = k.KGConnID
			}
			promptOverride = strings.TrimSpace(k.KGPrompt)
		}
	}
	return connID, promptOverride
}

// persist 落库（同 KB 跨 doc 同名实体归一由 ReplaceKGForDoc 事务保证；claims 反查 chunk 做溯源）。
func (x *Extractor) persist(kbID, docID string, out *kgOut, chunks []*store.KnowledgeChunk) error {
	entities := make([]*store.KGEntity, 0, len(out.Entities))
	for _, e := range out.Entities {
		entities = append(entities, &store.KGEntity{ID: store.NewID(), KBID: kbID, DocID: docID,
			Name: e.Name, Type: e.Type, Description: e.Description})
	}
	rels := make([]*store.KGRelationship, 0, len(out.Relations))
	for _, r := range out.Relations {
		rels = append(rels, &store.KGRelationship{ID: store.NewID(), KBID: kbID, DocID: docID,
			Source: r.Source, Target: r.Target, Type: r.Type})
	}
	claims := make([]*store.KGClaim, 0, len(out.Claims))
	for _, c := range out.Claims {
		claims = append(claims, &store.KGClaim{ID: store.NewID(), KBID: kbID, DocID: docID,
			ChunkID: claimChunkOf(c.Text, chunks), Subject: c.Subject, Text: c.Text})
	}
	return x.Store.ReplaceKGForDoc(kbID, docID, entities, rels, claims)
}

// claimChunkOf claim 文本反查出处 chunk（首 24 rune 命中即认；未命中留空 = 溯源缺失，不阻断）。
func claimChunkOf(text string, chunks []*store.KnowledgeChunk) string {
	head := []rune(text)
	if len(head) > 24 {
		head = head[:24]
	}
	for _, c := range chunks {
		if strings.Contains(c.Content, string(head)) {
			return c.ID
		}
	}
	return ""
}

func normalize(out *kgOut) {
	if out.Entities == nil {
		out.Entities = []struct {
			Name        string `json:"name"`
			Type        string `json:"type"`
			Description string `json:"description"`
		}{}
	}
	if out.Relations == nil {
		out.Relations = []struct {
			Source string `json:"source"`
			Target string `json:"target"`
			Type   string `json:"type"`
		}{}
	}
	if out.Claims == nil {
		out.Claims = []struct {
			Subject string `json:"subject"`
			Text    string `json:"text"`
		}{}
	}
}

// buildCorpus chunk 池 → 语料（doc 标记 + seq；超预算截断，对齐 ontobuild.buildCorpus 语义）。
func buildCorpus(chunks []*store.KnowledgeChunk, maxChunks, maxChars int) (string, bool) {
	var b strings.Builder
	used := 0
	truncated := false
	for _, c := range chunks {
		if used >= maxChunks || b.Len()+len(c.Content) > maxChars {
			truncated = true
			break
		}
		fmt.Fprintf(&b, "[doc:%s #%d] %s\n", c.DocID, c.Seq, strings.TrimSpace(c.Content))
		used++
	}
	return b.String(), truncated
}

// ---- 内置规则抽取（lightweight 回退；参考 semantica worker 内置轻量抽取的句式口径） ----

// ruleExtract 「A 是 B」→ IS_A、「A 的 B」→ 具有；claims = 含已知实体的句子。
// 纯字符串规则零依赖，保证 LLM 不可用时 graphrag 模式仍可用（教学演示兜底）。
func ruleExtract(chunks []*store.KnowledgeChunk) *kgOut {
	out := &kgOut{}
	entities := map[string]bool{}
	addEntity := func(name, typ, desc string) {
		name = strings.TrimSpace(name)
		if name == "" || len([]rune(name)) > 40 || entities[name] {
			return
		}
		entities[name] = true
		out.Entities = append(out.Entities, struct {
			Name        string `json:"name"`
			Type        string `json:"type"`
			Description string `json:"description"`
		}{Name: name, Type: typ, Description: desc})
	}
	sents := []string{}
	for _, c := range chunks {
		sents = append(sents, splitSentences(c.Content)...)
	}
	relSeen := map[string]bool{}
	addRel := func(source, target, typ string) {
		key := typ + "|" + source + "|" + target
		if source == "" || target == "" || source == target || relSeen[key] {
			return
		}
		relSeen[key] = true
		out.Relations = append(out.Relations, struct {
			Source string `json:"source"`
			Target string `json:"target"`
			Type   string `json:"type"`
		}{Source: source, Target: target, Type: typ})
	}
	for _, s := range sents {
		// 「A 是(一种/一类)B」
		for _, kw := range []string{"是一种", "是一类", "是一个", "是指", "是"} {
			if i := strings.Index(s, kw); i > 0 {
				a, b := strings.TrimSpace(s[:i]), trimTail(strings.TrimSpace(s[i+len(kw):]))
				if a != "" && b != "" && len([]rune(b)) <= 30 {
					addEntity(a, "concept", s)
					addEntity(b, "concept", "")
					addRel(a, b, "IS_A")
					out.Claims = append(out.Claims, struct {
						Subject string `json:"subject"`
						Text    string `json:"text"`
					}{Subject: a, Text: s})
					break
				}
			}
		}
		// 「A 的 B」→ A 具有 B
		if i := strings.Index(s, "的"); i > 0 && i < len(s)-3 {
			a := headOf(strings.TrimSpace(s[:i]))
			b := trimTail(strings.TrimSpace(s[i+3:]))
			if a != "" && b != "" && len([]rune(b)) <= 20 {
				addEntity(a, "concept", "")
				addEntity(b, "property", "")
				addRel(a, b, "具有")
			}
		}
	}
	// claims：含已知实体的句子补录（每实体至多 2 条）
	perEntity := map[string]int{}
	for _, s := range sents {
		for name := range entities {
			if perEntity[name] >= 2 || !strings.Contains(s, name) || len([]rune(s)) < 6 {
				continue
			}
			perEntity[name]++
			out.Claims = append(out.Claims, struct {
				Subject string `json:"subject"`
				Text    string `json:"text"`
			}{Subject: name, Text: s})
		}
	}
	if len(out.Claims) > 60 {
		out.Claims = out.Claims[:60]
	}
	return out
}

// splitSentences 粗分句（中文标点 + 换行）。
func splitSentences(text string) []string {
	raw := strings.FieldsFunc(text, func(r rune) bool {
		return r == '。' || r == '！' || r == '？' || r == '；' || r == '\n' || r == ';'
	})
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		if s = strings.TrimSpace(s); len([]rune(s)) >= 4 {
			out = append(out, s)
		}
	}
	return out
}

// headOf 取短语主干（截到最后一个分隔词前；教学口径粗规则，不求完备）。
func headOf(s string) string {
	for _, sep := range []string{"的", "与", "和", "及"} {
		if i := strings.LastIndex(s, sep); i > 0 {
			return s[:i]
		}
	}
	return s
}

// trimTail 去句尾连接词/标点残留。
func trimTail(s string) string {
	s = strings.TrimRight(s, "，。、；：,.;:")
	for _, sep := range []string{"其中", "并且", "而且", "同时", "通常", "一般"} {
		if i := strings.Index(s, sep); i > 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}
