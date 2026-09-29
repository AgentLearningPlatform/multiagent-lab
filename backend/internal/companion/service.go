package companion

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// ---------------------------------------------------------------------------
// REQ-170/M28 伴生 worker（旁路管线 M1）：Run/Resume 收尾触发 + message 表游标续抽。
// 低侵入三原则：只读消息库（不进 Eino ADK run 链路）；Agent 开关默认关；失败仅日志。
// ---------------------------------------------------------------------------

// companionSchema LLM 结构化抽取契约（薄本体：概念/关系/事件，confidence + 原文锚点）。
const companionSchema = `{
  "type": "object",
  "properties": {
    "concepts": {"type": "array", "items": {"type": "object", "properties": {
      "name": {"type": "string"}, "definition": {"type": "string"},
      "confidence": {"type": "number"}, "source": {"type": "string"}}, "required": ["name"]}},
    "relations": {"type": "array", "items": {"type": "object", "properties": {
      "rel_name": {"type": "string"}, "source": {"type": "string"}, "target": {"type": "string"},
      "definition": {"type": "string"}, "confidence": {"type": "number"}, "evidence": {"type": "string"}},
      "required": ["rel_name", "source", "target"]}},
    "events": {"type": "array", "items": {"type": "object", "properties": {
      "name": {"type": "string"}, "definition": {"type": "string"},
      "confidence": {"type": "number"}, "source": {"type": "string"}}, "required": ["name"]}}
  },
  "required": ["concepts", "relations", "events"]
}`

// extractOut 抽取输出结构。
type extractOut struct {
	Concepts []struct {
		Name       string  `json:"name"`
		Definition string  `json:"definition"`
		Confidence float64 `json:"confidence"`
		Source     string  `json:"source"`
	} `json:"concepts"`
	Relations []struct {
		RelName    string  `json:"rel_name"`
		Source     string  `json:"source"`
		Target     string  `json:"target"`
		Definition string  `json:"definition"`
		Confidence float64 `json:"confidence"`
		Evidence   string  `json:"evidence"`
	} `json:"relations"`
	Events []struct {
		Name       string  `json:"name"`
		Definition string  `json:"definition"`
		Confidence float64 `json:"confidence"`
		Source     string  `json:"source"`
	} `json:"events"`
}

// companionPrompt 抽取提示词（教学口径：只抽确证的领域事实，宁缺毋滥）。
// alignment REQ-194①：已有实体清单约束节（空串=无清单走原行为）。
func companionPrompt(corpus, hint, alignment string) string {
	var b strings.Builder
	b.WriteString("你是本体候选抽取助手。阅读以下对话片段，抽取其中值得沉淀为知识的领域概念、概念间关系与事件。\n")
	b.WriteString("要求：\n")
	b.WriteString("1. concepts：领域实体/术语（如 Pod、滚动更新、淋巴结局限性切除），name 用唯一中文短语，definition 一句话，confidence 0~1。\n")
	b.WriteString("2. relations：概念间有意义的关联，rel_name 用动名词（如「引发」「适用于」「依赖」），source/target 引用 concepts 中的 name，evidence 为原文依据短句。\n")
	b.WriteString("3. events：带时间性的动作/变更/结论（如「2026-09 完成灰度切换」）。\n")
	b.WriteString("4. 只抽取对话中明确陈述的事实，不要推测；没有可抽内容就返回三个空数组。\n")
	if strings.TrimSpace(hint) != "" {
		// REQ-187：领域聚焦提示（agent 级配置）——追加领域抽取标准
		b.WriteString("5. 领域聚焦要求（优先级最高）：" + strings.TrimSpace(hint) + "\n")
	}
	if alignment != "" {
		b.WriteString(alignment)
	}
	b.WriteString("只输出 JSON，不要输出其他内容。对话片段：\n")
	b.WriteString(corpus)
	return b.String()
}

// ---------------------------------------------------------------------------
// REQ-194/M34 批次一③：分窗抽取（治 G4 增量一次性拼接无分窗——与 KB kg.go 24k 硬截断同型）。
// 每窗 ≤16 条消息且 ≤8k 字符（先到为准）；单次触发最多 3 窗，超出部分游标停在第 3 窗末，
// 超长积压由后续 Run 收尾自然续抽（游标语义不变）；失败停在上一成功窗末（可重试）。
// ---------------------------------------------------------------------------

const (
	windowMaxMsgs    = 16
	windowMaxChars   = 8000
	maxWindowsPerRun = 3
)

// extractWindow 一个抽取窗：消息切片 + 窗末消息 id（游标推进锚点）。
type extractWindow struct {
	msgs   []*store.Message
	lastID string
}

// splitWindows 增量消息分窗（纯函数；窗尺寸按渲染后语料字符计，与实际送 LLM 的文本同源）。
func splitWindows(msgs []*store.Message) []extractWindow {
	var wins []extractWindow
	var cur []*store.Message
	size := 0
	flush := func() {
		if len(cur) > 0 {
			wins = append(wins, extractWindow{msgs: cur, lastID: cur[len(cur)-1].ID})
			cur, size = nil, 0
		}
	}
	for _, m := range msgs {
		w := len([]rune(renderCorpus([]*store.Message{m})))
		if len(cur) > 0 && (len(cur)+1 > windowMaxMsgs || size+w > windowMaxChars) {
			flush()
		}
		cur = append(cur, m)
		size += w
	}
	flush()
	return wins
}

// renderCorpus 语料拼接（含消息 id 锚点，供溯源字段落候选）。
func renderCorpus(msgs []*store.Message) string {
	var corpus strings.Builder
	for _, m := range msgs {
		fmt.Fprintf(&corpus, "[%s] %s：%s\n", m.ID, roleLabel(m.Role), truncate(m.Content, 600))
	}
	return corpus.String()
}

// remainingAfter 游标增量定位：lastID 之后的未处理消息（lastID 不在列表 = 历史已清理，保守返回空防重抽）。
func remainingAfter(msgs []*store.Message, lastID string) []*store.Message {
	start := 0 // 空游标（首次抽取）= 全量
	if lastID != "" {
		start = len(msgs) // 游标不在列表（历史已清理）= 保守空，防重抽
		for i, m := range msgs {
			if m.ID == lastID {
				start = i + 1
				break
			}
		}
	}
	var fresh []*store.Message
	for _, m := range msgs[start:] {
		if (m.Role == "user" || m.Role == "assistant") && strings.TrimSpace(m.Content) != "" {
			fresh = append(fresh, m)
		}
	}
	return fresh
}

// Service 伴生本体服务（worker + 候选编排 + 伴生图写入）。
type Service struct {
	Store  *store.Store
	Box    *secrets.Box
	Engine *Engine

	mu      sync.Mutex // 串行化同会话抽取（收尾事件可能并发到达）
	running map[string]bool

	// REQ-194②召回增强：进程内标签向量缓存（convID → label → vector；会话图写入时失效）
	vecMu    sync.Mutex
	vecCache map[string]map[string][]float32
}

// NewService 构造（engine 为空则用默认目录/端口）。
func NewService(st *store.Store, box *secrets.Box, engine *Engine) *Service {
	if engine == nil {
		engine = NewEngine(osBinary(), "", 0)
	}
	return &Service{Store: st, Box: box, Engine: engine, running: map[string]bool{}, vecCache: map[string]map[string][]float32{}}
}

// invalidateLabelCache 会话图写入后失效标签向量缓存（图标签稳态缓存——避免每轮全量重算）。
func (s *Service) invalidateLabelCache(convID string) {
	if s == nil {
		return
	}
	s.vecMu.Lock()
	delete(s.vecCache, convID)
	s.vecMu.Unlock()
}

// OnRunComplete Run/Resume 收尾触发点（API 层调用；非阻塞、零错误上抛）。
// 开关关闭 / 会话与 Agent 归属不符 → 静默返回，对话主链路无感知。
// 归属校验（2026-09-27 修复：项目会话此前被 Scope 守卫整类拦截，候选从不产生）：
//   - agent 会话：conv.AgentID == agent.ID；
//   - project 会话：agent 为该项目的 coordinator 或成员之一（运行 agent 由 resolveRunTarget
//     按主智能体优先解析传入）——「项目由开启伴生的 agent 管理」时项目处理信息同样产生候选。
func (s *Service) OnRunComplete(conv *store.Conversation, agent *store.Agent) {
	if s == nil || conv == nil || agent == nil {
		return
	}
	if !agent.CompanionOntology {
		return
	}
	switch conv.Scope {
	case "agent":
		if conv.AgentID == nil || *conv.AgentID != agent.ID {
			return
		}
	case "project":
		if conv.ProjectID == nil || *conv.ProjectID == "" {
			return
		}
		p, err := s.Store.GetProject(*conv.ProjectID)
		if err != nil {
			return
		}
		member := p.Coordinator == agent.ID
		if !member {
			for _, id := range p.AgentIDs {
				if id == agent.ID {
					member = true
					break
				}
			}
		}
		if !member {
			return
		}
	default:
		return
	}
	s.mu.Lock()
	if s.running[conv.ID] {
		s.mu.Unlock()
		return
	}
	s.running[conv.ID] = true
	s.mu.Unlock()
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.running, conv.ID)
			s.mu.Unlock()
			if r := recover(); r != nil {
				log.Printf("[companion] 抽取 panic（会话 %s）: %v", conv.ID, r)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		n, err := s.ExtractNew(ctx, conv.ID, agent.ID)
		if err != nil {
			log.Printf("[companion] 会话 %s 抽取失败（不影响对话）: %v", conv.ID, err)
			return
		}
		if n > 0 {
			log.Printf("[companion] 会话 %s 新增 %d 条候选待确认", conv.ID, n)
		}
	}()
}

// extractConnID 抽取/判定模型连接（REQ-187：companion 覆盖优先，空=跟随 agent 模型连接）。
func extractConnID(agent *store.Agent) string {
	if agent == nil {
		return ""
	}
	if agent.CompanionExtractConnID != "" {
		return agent.CompanionExtractConnID
	}
	if agent.ModelConnID != nil {
		return *agent.ModelConnID
	}
	return ""
}

// knownEntityLabels 会话伴生图已有实体标签（REQ-194①对齐清单数据源）。
// 引擎不在位（未启动且无存活实例）→ 空（新会话/读侧不拉起，走原行为）；
// 查询失败 → 空+日志（低侵入三原则：失败仅日志，不阻断抽取）。
func (s *Service) knownEntityLabels(ctx context.Context, convID string) []string {
	if s == nil || s.Engine == nil {
		return nil
	}
	if s.Engine.Endpoint() == "" && !s.Engine.AdoptRunning(ctx) {
		return nil
	}
	raw, err := s.Engine.Query(ctx, SelectLabels(convID))
	if err != nil {
		log.Printf("[companion] 已有实体清单查询失败（按空清单抽取）: %v", err)
		return nil
	}
	return parseLabelValues(raw)
}

// ExtractNew 游标续抽（REQ-194③分窗）：读新消息 → 分窗 → 逐窗 LLM 结构化抽取 → 候选落库 →
// 游标逐窗推进。返回新增候选数。窗口语义：每窗 ≤16 条且 ≤8k 字符、单次最多 3 窗；
// 上一窗实体进下窗对齐清单（跨窗归并）；某窗失败 → 游标停在上一成功窗末（可重试）。
func (s *Service) ExtractNew(ctx context.Context, convID, agentID string) (int, error) {
	agent, err := s.Store.GetAgent(agentID)
	if err != nil {
		return 0, err
	}
	cursor, err := s.Store.GetCompanionCursor(convID)
	if err != nil {
		return 0, err
	}
	msgs, err := s.Store.ListMessages(convID)
	if err != nil {
		return 0, err
	}
	// 游标定位：last_message_id 之后的增量（只抽用户/助手文本消息）
	fresh := remainingAfter(msgs, cursor.LastMessageID)
	if len(fresh) == 0 {
		return 0, nil
	}

	connID := extractConnID(agent)
	// REQ-194①：已有实体清单（引擎不在位/查询失败=空清单，走原行为）
	known := s.knownEntityLabels(ctx, convID)

	windows := splitWindows(fresh)
	if len(windows) > maxWindowsPerRun {
		// 超长积压：本轮只处理前 3 窗，游标停在第 3 窗末，后续 Run 自然续抽
		windows = windows[:maxWindowsPerRun]
	}
	total := 0
	for i, win := range windows {
		res, err := chat.GenerateStructured(ctx, s.Store, s.Box, connID,
			companionPrompt(renderCorpus(win.msgs), agent.CompanionExtractHint, buildAlignmentSection(known)), companionSchema)
		if err != nil {
			return total, fmt.Errorf("LLM 抽取失败（第 %d/%d 窗，游标停在上一成功窗末可重试）: %w", i+1, len(windows), err)
		}
		var out extractOut
		if err := json.Unmarshal(res.DraftJSON, &out); err != nil {
			return total, fmt.Errorf("抽取输出解析失败（第 %d/%d 窗）: %w", i+1, len(windows), err)
		}

		cands := toCandidates(convID, agentID, win.msgs, &out)
		markAligned(cands, known) // REQ-194①：对齐标记落库
		if err := s.Store.CreateCompanionCandidates(cands); err != nil {
			return total, err
		}
		// REQ-187：置信度阈值自动入图（0=全人工审；≥阈值自动 confirmCandidate——含矛盾旧边失效化
		// 与 REQ-194⑤语义矛盾检测；自动入图走与人工确认完全相同的链路，区别仅在来源标记 bot:autoConfirmed）
		if agent.CompanionAutoThreshold > 0 {
			for _, c := range cands {
				if c.Confidence >= agent.CompanionAutoThreshold {
					if _, err := s.ConfirmCandidate(ctx, c.ID); err != nil {
						log.Printf("[companion] 自动入图失败（候选 %s，不影响其余候选）: %v", c.ID, err)
						continue
					}
					// bot:autoConfirmed 溯源标记（区分自动入图与人工确认）
					_ = s.Engine.Update(ctx, MarkAutoConfirmed(convID, c.ID))
					log.Printf("[companion] 候选 %s 置信 %.2f ≥ 阈值 %.2f，已自动入图", c.Name, c.Confidence, agent.CompanionAutoThreshold)
				}
			}
		}
		// 跨窗对齐：本窗产物实体并入下窗清单（REQ-194③）
		known = appendWindowEntities(known, cands)
		total += len(cands)
		// 游标推进到本窗末（逐窗推进：失败停在上一成功窗末）
		if err := s.Store.AdvanceCompanionCursor(convID, win.lastID); err != nil {
			return total, err
		}
	}
	return total, nil
}

// toCandidates LLM 输出 → 候选记录（evidence/name 回链最近包含该文本的消息 id）。
func toCandidates(convID, agentID string, msgs []*store.Message, out *extractOut) []*store.CompanionCandidate {
	anchor := func(text string) (string, string) {
		if text != "" {
			for _, m := range msgs {
				if strings.Contains(m.Content, text) {
					return m.ID, truncate(text, 120)
				}
			}
		}
		// 兜底：锚定最后一条助手消息
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Role == "assistant" {
				return msgs[i].ID, truncate(text, 120)
			}
		}
		return msgs[len(msgs)-1].ID, truncate(text, 120)
	}
	var cands []*store.CompanionCandidate
	add := func(c *store.CompanionCandidate) {
		c.ConversationID, c.AgentID, c.Status = convID, agentID, "pending"
		cands = append(cands, c)
	}
	for _, c := range out.Concepts {
		if strings.TrimSpace(c.Name) == "" {
			continue
		}
		mid, excerpt := anchor(c.Source)
		add(&store.CompanionCandidate{Kind: "concept", Name: truncate(c.Name, 120), Definition: truncate(c.Definition, 500), Confidence: c.Confidence, SourceMessageID: mid, SourceExcerpt: excerpt})
	}
	for _, r := range out.Relations {
		if strings.TrimSpace(r.RelName) == "" || strings.TrimSpace(r.Source) == "" || strings.TrimSpace(r.Target) == "" {
			continue
		}
		mid, excerpt := anchor(r.Evidence)
		// relation：name=主体可读态、rel_name=关系名、rel_target=目标概念（入图按三件拆）
		add(&store.CompanionCandidate{Kind: "relation", Name: truncate(r.Source, 120), RelName: truncate(r.RelName, 120), RelTarget: truncate(r.Target, 120), Definition: truncate(r.Definition, 500), Confidence: r.Confidence, SourceMessageID: mid, SourceExcerpt: excerpt})
	}
	for _, e := range out.Events {
		if strings.TrimSpace(e.Name) == "" {
			continue
		}
		mid, excerpt := anchor(e.Source)
		add(&store.CompanionCandidate{Kind: "event", Name: truncate(e.Name, 120), Definition: truncate(e.Definition, 500), Confidence: e.Confidence, SourceMessageID: mid, SourceExcerpt: excerpt})
	}
	return cands
}

// ConfirmCandidate 候选确认 → 入会话图（种子 schema 幂等预置 + INSERT + 矛盾旧边失效化）。
// REQ-194⑤：同主体+同关系名走确定性失效化（既有路径）；不同关系名的语义冲突交 LLM 二分类
// （冲突才 invalidAt；不确定双保留 + 候选 note「疑似矛盾待人工」；失败仅日志不阻断入图）。
func (s *Service) ConfirmCandidate(ctx context.Context, candID string) (*store.CompanionCandidate, error) {
	c, err := s.Store.GetCompanionCandidate(candID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if err := s.Engine.Update(ctx, SeedSchema()); err != nil {
		return nil, fmt.Errorf("种子 schema 预置失败: %w", err)
	}
	if c.Kind == "relation" {
		// 确定性矛盾：同主体+同关系名+未失效旧边 → invalidAt 标记（失效化而非删除）
		raw, err := s.Engine.Query(ctx, FindActiveEdge(c.ConversationID, c.Name, c.RelName))
		if err != nil {
			return nil, fmt.Errorf("矛盾检测查询失败: %w", err)
		}
		if edge := parseEdgeURI(raw); edge != "" {
			if err := s.Engine.Update(ctx, InvalidateEdge(c.ConversationID, edge, now)); err != nil {
				return nil, fmt.Errorf("旧边失效化失败: %w", err)
			}
		}
		// REQ-194⑤：语义矛盾二分类（LLM 增强路径，失败/无连接静默跳过）
		s.semanticConflictCheck(ctx, c, now)
		if err := s.Engine.Update(ctx, InsertRelationTriples(c.ConversationID, c.ID, c.RelName, c.Name, c.RelTarget, c.Definition, c.Confidence, c.SourceMessageID, now)); err != nil {
			return nil, fmt.Errorf("关系入图失败: %w", err)
		}
	} else {
		if err := s.Engine.Update(ctx, InsertNodeTriples(c.ConversationID, c.ID, c.Kind, c.Name, c.Definition, c.Confidence, c.SourceMessageID, now)); err != nil {
			return nil, fmt.Errorf("入图失败: %w", err)
		}
	}
	s.invalidateLabelCache(c.ConversationID) // REQ-194②：图写入失效标签向量缓存
	return s.Store.DecideCompanionCandidate(candID, "confirmed")
}

// semanticConflictCheck REQ-194⑤语义矛盾检测：同主体活跃断言与新断言拼 prompt 交 LLM 二分类。
// yes → 被冲突旧边 invalidAt；unsure → 双保留 + 候选 note「疑似矛盾待人工」（审计可查）。
// 引擎/LLM 失败仅日志（矛盾检测是增强不是门禁，不阻断入图）。
func (s *Service) semanticConflictCheck(ctx context.Context, c *store.CompanionCandidate, at time.Time) {
	raw, err := s.Engine.Query(ctx, SelectSubjectActiveEdges(c.ConversationID, c.Name))
	if err != nil {
		log.Printf("[companion] 同主体活跃边查询失败（跳过语义矛盾检测，候选 %s）: %v", c.ID, err)
		return
	}
	existing := parseSubjectEdges(raw)
	if len(existing) == 0 {
		return
	}
	agent, err := s.Store.GetAgent(c.AgentID)
	if err != nil {
		return
	}
	connID := extractConnID(agent)
	if connID == "" {
		return // 无模型连接无法判定，保留现状（确定性路径仍在）
	}
	res, err := chat.GenerateStructured(ctx, s.Store, s.Box, connID, conflictPrompt(c.Name, existing, c.RelName, c.RelTarget), conflictSchema)
	if err != nil {
		log.Printf("[companion] 语义矛盾判定失败（保留双方，候选 %s）: %v", c.ID, err)
		return
	}
	var out conflictOut
	if err := json.Unmarshal(res.DraftJSON, &out); err != nil {
		log.Printf("[companion] 语义矛盾判定解析失败（保留双方，候选 %s）: %v", c.ID, err)
		return
	}
	switch out.Conflict {
	case "yes":
		edge := matchConflictEdge(existing, out.ConflictRelName, out.ConflictObject)
		if edge == "" {
			return
		}
		if err := s.Engine.Update(ctx, InvalidateEdge(c.ConversationID, edge, at)); err != nil {
			log.Printf("[companion] 冲突旧边失效化失败（候选 %s）: %v", c.ID, err)
			return
		}
		log.Printf("[companion] 语义矛盾：候选 %s（%s —%s→ %s）与既有断言冲突，旧边已失效化", c.ID, c.Name, c.RelName, c.RelTarget)
	case "unsure":
		note := "疑似矛盾待人工：" + truncate(out.Reason, 160)
		if err := s.Store.SetCompanionCandidateNote(c.ID, note); err != nil {
			log.Printf("[companion] 矛盾注记回写失败（候选 %s）: %v", c.ID, err)
		}
	}
}

// parseSubjectEdges SelectSubjectActiveEdges 结果 → 断言行。
func parseSubjectEdges(raw []byte) []edgeAssertion {
	var res struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil
	}
	out := make([]edgeAssertion, 0, len(res.Results.Bindings))
	for _, b := range res.Results.Bindings {
		out = append(out, edgeAssertion{Edge: b["edge"].Value, RelName: b["relName"].Value, ObjLabel: b["objLabel"].Value})
	}
	return out
}

// RejectCandidate 候选拒绝（不触达伴生图）。
func (s *Service) RejectCandidate(ctx context.Context, candID string) (*store.CompanionCandidate, error) {
	return s.Store.DecideCompanionCandidate(candID, "rejected")
}

// GraphNode / GraphEdge 成长可视化数据（REQ-154；3d-force-graph 前端渲染）。
type GraphNode struct {
	Label      string  `json:"label"`
	Kind       string  `json:"kind"` // Concept | Event
	Definition string  `json:"definition,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	CreatedAt  string  `json:"created_at,omitempty"`
}

type GraphEdge struct {
	Source    string `json:"source"`
	Target    string `json:"target"`
	Rel       string `json:"rel"`
	CreatedAt string `json:"created_at,omitempty"`
}

// Graph 会话伴生图全量读取（REQ-154 成长可视化数据源；节点=概念/事件实体，边=活跃关系）。
// 引擎不在位（未启动且无存活实例）返回空图——读侧不拉起（与检索同口径）。
func (s *Service) Graph(ctx context.Context, convID string) (map[string]any, error) {
	out := map[string]any{"conversation_id": convID, "graph": GraphURI(convID), "nodes": []GraphNode{}, "edges": []GraphEdge{}, "engine_running": false}
	if s.Engine.Endpoint() == "" && !s.Engine.AdoptRunning(ctx) {
		return out, nil
	}
	out["engine_running"] = true
	nodes := []GraphNode{}
	raw, err := s.Engine.Query(ctx, SelectNodes(convID))
	if err == nil {
		var res struct {
			Results struct {
				Bindings []map[string]struct {
					Value string `json:"value"`
				} `json:"bindings"`
			} `json:"results"`
		}
		if json.Unmarshal(raw, &res) == nil {
			for _, b := range res.Results.Bindings {
				conf := 0.0
				fmt.Sscanf(b["conf"].Value, "%f", &conf)
				kind := strings.TrimPrefix(b["kind"].Value, BotNS)
				nodes = append(nodes, GraphNode{Label: b["label"].Value, Kind: kind, Definition: b["def"].Value, Confidence: conf, CreatedAt: b["at"].Value})
			}
		}
	}
	edges := []GraphEdge{}
	raw, err = s.Engine.Query(ctx, SelectEdges(convID))
	if err == nil {
		var res struct {
			Results struct {
				Bindings []map[string]struct {
					Value string `json:"value"`
				} `json:"bindings"`
			} `json:"results"`
		}
		if json.Unmarshal(raw, &res) == nil {
			for _, b := range res.Results.Bindings {
				edges = append(edges, GraphEdge{Source: b["src"].Value, Target: b["dst"].Value, Rel: b["rel"].Value, CreatedAt: b["at"].Value})
			}
		}
	}
	out["nodes"], out["edges"] = nodes, edges
	return out, nil
}

// ResetConversation 会话级整体摘除：DROP GRAPH + 清候选/游标 +（可选）停引擎。
func (s *Service) ResetConversation(ctx context.Context, convID string) error {
	if err := s.Engine.Update(ctx, DropGraph(convID)); err != nil {
		return err
	}
	s.invalidateLabelCache(convID)
	return s.Store.DeleteConversationCompanionData(convID)
}

// Status 伴生管线状态（引擎端点/游标/pending 计数/实体标签）。
func (s *Service) Status(ctx context.Context, convID string) (map[string]any, error) {
	cursor, _ := s.Store.GetCompanionCursor(convID)
	pending, _ := s.Store.ListCompanionCandidates(convID, "", "pending")
	st := map[string]any{
		"conversation_id": convID,
		"cursor":          cursor,
		"pending_count":   len(pending),
		"graph":           GraphURI(convID),
		"engine_running":  s.Engine.Endpoint() != "",
		"engine_endpoint": s.Engine.Endpoint(),
	}
	// REQ-195：引擎加载路径可观测——实际二进制/数据目录/端点透出（加载路径 vs 实际路径页面上可见）
	if ep := s.Engine.Endpoint(); ep != "" {
		st["engine_detail"] = map[string]any{
			"binary":   s.Engine.ResolvedBinary(),
			"data_dir": s.Engine.DataDir,
			"endpoint": ep,
		}
	}
	if s.Engine.Endpoint() != "" {
		if raw, err := s.Engine.Query(ctx, SelectLabels(convID)); err == nil {
			st["labels"] = json.RawMessage(extractLabelsJSON(raw))
		}
	}
	return st, nil
}
