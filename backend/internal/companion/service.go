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
func companionPrompt(corpus string) string {
	return "你是本体候选抽取助手。阅读以下对话片段，抽取其中值得沉淀为知识的领域概念、概念间关系与事件。\n" +
		"要求：\n" +
		"1. concepts：领域实体/术语（如 Pod、滚动更新、淋巴结局限性切除），name 用唯一中文短语，definition 一句话，confidence 0~1。\n" +
		"2. relations：概念间有意义的关联，rel_name 用动名词（如「引发」「适用于」「依赖」），source/target 引用 concepts 中的 name，evidence 为原文依据短句。\n" +
		"3. events：带时间性的动作/变更/结论（如「2026-09 完成灰度切换」）。\n" +
		"4. 只抽取对话中明确陈述的事实，不要推测；没有可抽内容就返回三个空数组。\n" +
		"只输出 JSON，不要输出其他内容。对话片段：\n" + corpus
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
}

// NewService 构造（engine 为空则用默认目录/端口）。
func NewService(st *store.Store, box *secrets.Box, engine *Engine) *Service {
	if engine == nil {
		engine = NewEngine(osBinary(), "", 0)
	}
	return &Service{Store: st, Box: box, Engine: engine, running: map[string]bool{}}
}

// OnRunComplete Run/Resume 收尾触发点（API 层调用；非阻塞、零错误上抛）。
// 开关关闭 / 非会话绑定 Agent / 引擎不可用 → 静默返回，对话主链路无感知。
func (s *Service) OnRunComplete(conv *store.Conversation, agent *store.Agent) {
	if s == nil || conv == nil || agent == nil {
		return
	}
	if !agent.CompanionOntology {
		return
	}
	if conv.Scope != "agent" || conv.AgentID == nil || *conv.AgentID != agent.ID {
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

// ExtractNew 游标续抽：读新消息 → LLM 结构化抽取 → 候选落库 → 游标推进。返回新增候选数。
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

	// 语料拼接（含消息 id 锚点，供溯源字段落候选）
	var corpus strings.Builder
	for _, m := range fresh {
		fmt.Fprintf(&corpus, "[%s] %s：%s\n", m.ID, roleLabel(m.Role), truncate(m.Content, 600))
	}

	var connID string
	if agent.ModelConnID != nil {
		connID = *agent.ModelConnID
	}
	res, err := chat.GenerateStructured(ctx, s.Store, s.Box, connID, companionPrompt(corpus.String()), companionSchema)
	if err != nil {
		return 0, fmt.Errorf("LLM 抽取失败: %w", err)
	}
	var out extractOut
	if err := json.Unmarshal(res.DraftJSON, &out); err != nil {
		return 0, fmt.Errorf("抽取输出解析失败: %w", err)
	}

	cands := toCandidates(convID, agentID, fresh, &out)
	if err := s.Store.CreateCompanionCandidates(cands); err != nil {
		return 0, err
	}
	// 游标推进到最后一条已读消息（无论是否有产出）
	if err := s.Store.AdvanceCompanionCursor(convID, msgs[len(msgs)-1].ID); err != nil {
		return 0, err
	}
	return len(cands), nil
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
		// 矛盾检测：同主体+同关系名+未失效旧边 → invalidAt 标记（失效化而非删除）
		raw, err := s.Engine.Query(ctx, FindActiveEdge(c.ConversationID, c.Name, c.RelName))
		if err != nil {
			return nil, fmt.Errorf("矛盾检测查询失败: %w", err)
		}
		if edge := parseEdgeURI(raw); edge != "" {
			if err := s.Engine.Update(ctx, InvalidateEdge(c.ConversationID, edge, now)); err != nil {
				return nil, fmt.Errorf("旧边失效化失败: %w", err)
			}
		}
		if err := s.Engine.Update(ctx, InsertRelationTriples(c.ConversationID, c.ID, c.RelName, c.Name, c.RelTarget, c.Definition, c.Confidence, c.SourceMessageID, now)); err != nil {
			return nil, fmt.Errorf("关系入图失败: %w", err)
		}
	} else {
		if err := s.Engine.Update(ctx, InsertNodeTriples(c.ConversationID, c.ID, c.Kind, c.Name, c.Definition, c.Confidence, c.SourceMessageID, now)); err != nil {
			return nil, fmt.Errorf("入图失败: %w", err)
		}
	}
	return s.Store.DecideCompanionCandidate(candID, "confirmed")
}

// RejectCandidate 候选拒绝（不触达伴生图）。
func (s *Service) RejectCandidate(ctx context.Context, candID string) (*store.CompanionCandidate, error) {
	return s.Store.DecideCompanionCandidate(candID, "rejected")
}

// ResetConversation 会话级整体摘除：DROP GRAPH + 清候选/游标 +（可选）停引擎。
func (s *Service) ResetConversation(ctx context.Context, convID string) error {
	if err := s.Engine.Update(ctx, DropGraph(convID)); err != nil {
		return err
	}
	return s.Store.DeleteConversationCompanionData(convID)
}

// Status 伴生管线状态（引擎端点/游标/pending 计数/实体标签）。
func (s *Service) Status(ctx context.Context, convID string) (map[string]any, error) {
	cursor, _ := s.Store.GetCompanionCursor(convID)
	pending, _ := s.Store.ListCompanionCandidates(convID, "pending")
	st := map[string]any{
		"conversation_id": convID,
		"cursor":          cursor,
		"pending_count":   len(pending),
		"graph":           GraphURI(convID),
		"engine_running":  s.Engine.Endpoint() != "",
		"engine_endpoint": s.Engine.Endpoint(),
	}
	if s.Engine.Endpoint() != "" {
		if raw, err := s.Engine.Query(ctx, SelectLabels(convID)); err == nil {
			st["labels"] = json.RawMessage(extractLabelsJSON(raw))
		}
	}
	return st, nil
}
