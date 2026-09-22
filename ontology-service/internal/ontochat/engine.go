// engine.go OntoChat 多轮引导引擎（REQ-103 模式 A）。
// 职责：按会话阶段组装 prompt → 经 llmcreate.Creator 调主平台生成 → 解析意图 → 推进状态机。
// 阶段语义（与 14 号方案 §5 OntoChat 流程页一致）：
//   cq     首轮：用户给领域描述 + 能力问题列表 → 引擎确认并提示下一步
//   domain 补全轮：用户逐轮补充领域信息 → 引擎归纳要点，询问是否足够生成
//   draft  生成轮：把累积上下文整体喂给生成器产出 spec 草稿（复用 llmcreate 校验循环）
//   refine 修正轮：草稿校验错误回喂 → 重新生成（引擎内已含，最多 3 轮）
package ontochat

import (
	"encoding/json"
	"fmt"
	"strings"

	pkgspec "github.com/xiaoyao/eino-multiagent-lab/pkg/ontology/spec"

	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/llmcreate"
)

// Engine 多轮引导引擎（复用 llmcreate.Creator 的平台代理与校验循环）。
type Engine struct {
	LLM *llmcreate.Creator
}

// TurnResult 单轮交互结果：assistant 回复 + 可选草稿（draft/refine 阶段产出）。
type TurnResult struct {
	Reply     string        // assistant 消息
	NextStage string        // 推进后的阶段
	Draft     *pkgspec.Spec // 非 nil 表示本轮产出了草稿（含校验警告）
	Warning   string        // 草稿带警告（如达最大修正轮数仍有错误）
}

// domainSufficientPrompt 补全轮的归纳 prompt：让模型归纳已给信息并判断是否可生成。
const domainSufficientPrompt = `你是本体建模访谈专家。用户正在逐步补充领域信息，请：
1. 用 2~4 条要点归纳用户本轮补充的信息（不要重复已有要点）；
2. 判断当前信息是否足以生成一个可用的本体草稿；
3. 若不足，明确列出还缺什么（如：关键概念间的关系、实例来源、层级深度），用一句话引导用户补充；
4. 若已足够，回复以「可以生成」开头，并简述你准备建模的概念范围。
要求：中文、简洁（150 字内）、不要输出 JSON。`

// Turn 处理一轮用户输入，推进状态机并落库。
func (e *Engine) Turn(st *Store, sess *Session, userText string) (*TurnResult, error) {
	switch sess.Stage {
	case "cq":
		return e.turnCQ(st, sess, userText)
	case "domain":
		return e.turnDomain(st, sess, userText)
	case "draft", "refine":
		return e.turnDraft(st, sess)
	default:
		return nil, fmt.Errorf("会话已结束（stage=done），如需继续请新建会话")
	}
}

// turnCQ 首轮：领域描述（必填）+ 能力问题（每行一个，可选）。
func (e *Engine) turnCQ(st *Store, sess *Session, userText string) (*TurnResult, error) {
	text := strings.TrimSpace(userText)
	if text == "" {
		return nil, fmt.Errorf("请先描述要建模的领域")
	}
	// 约定格式：首行为领域描述，其余行（或「能力问题：」后的行）为 CQ
	lines := strings.Split(text, "\n")
	desc := strings.TrimSpace(lines[0])
	cqs := []string{}
	inCQ := false
	for _, ln := range lines[1:] {
		t := strings.TrimSpace(ln)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "能力问题") {
			inCQ = true
			t = strings.TrimPrefix(strings.TrimPrefix(t, "能力问题"), "：")
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
		}
		if inCQ {
			cqs = append(cqs, strings.TrimLeft(t, "0123456789.、) "))
		} else if desc == "" {
			desc = t
		} else {
			cqs = append(cqs, strings.TrimLeft(t, "0123456789.、) "))
		}
	}
	if desc == "" {
		return nil, fmt.Errorf("未能识别领域描述，请把描述放在第一行")
	}
	sess.Context.Description = desc
	sess.Context.CQs = cqs

	var b strings.Builder
	b.WriteString("已记录领域描述：")
	b.WriteString(truncate(desc, 80))
	if len(cqs) > 0 {
		fmt.Fprintf(&b, "\n已记录能力问题 %d 条：", len(cqs))
		for i, q := range cqs {
			fmt.Fprintf(&b, "\n%d. %s", i+1, truncate(q, 60))
		}
	} else {
		b.WriteString("\n尚未提供能力问题（可选）。建议列 3~5 个本体要回答的问题，能显著提升建模质量；也可以直接进入补全阶段。")
	}
	b.WriteString("\n\n下一步：请继续补充领域信息（关键概念、层级、关系、实例来源等），补充充分后回复「生成草稿」即可产出 spec_json。")
	reply := b.String()
	stage := "domain"
	round := 0
	if err := st.Append(sess.ID, Message{Role: "assistant", Content: reply}, &stage, &round, &sess.Context); err != nil {
		return nil, err
	}
	return &TurnResult{Reply: reply, NextStage: stage}, nil
}

// turnDomain 补全轮：归纳要点；用户说「生成草稿」则直接进入 draft。
func (e *Engine) turnDomain(st *Store, sess *Session, userText string) (*TurnResult, error) {
	text := strings.TrimSpace(userText)
	if text == "" {
		return nil, fmt.Errorf("请输入内容：补充领域信息，或回复「生成草稿」")
	}
	if isGenerateIntent(text) {
		return e.turnDraft(st, sess)
	}
	sess.Context.Hints = append(sess.Context.Hints, text)

	// 归纳 prompt：把已累积上下文给模型，要要点归纳 + 是否足够判断
	var b strings.Builder
	b.WriteString(domainSufficientPrompt)
	b.WriteString("\n\n领域描述：\n" + sess.Context.Description)
	if len(sess.Context.CQs) > 0 {
		b.WriteString("\n\n能力问题：")
		for i, q := range sess.Context.CQs {
			fmt.Fprintf(&b, "\n%d. %s", i+1, q)
		}
	}
	if len(sess.Context.Hints) > 0 {
		b.WriteString("\n\n已补充的信息：")
		for i, h := range sess.Context.Hints {
			fmt.Fprintf(&b, "\n%d. %s", i+1, h)
		}
	}
	reply, _, err := e.LLM.RawChat(b.String())
	if err != nil {
		return nil, err
	}
	round := sess.Round + 1
	stage := "domain"
	if err := st.Append(sess.ID, Message{Role: "assistant", Content: reply}, &stage, &round, &sess.Context); err != nil {
		return nil, err
	}
	return &TurnResult{Reply: reply, NextStage: stage}, nil
}

// turnDraft 生成轮：累积上下文 → llmcreate.Draft（内含校验回喂循环，最多 3 轮）。
func (e *Engine) turnDraft(st *Store, sess *Session) (*TurnResult, error) {
	// 组装 extraHint：CQ + 逐轮补全要点
	var hints []string
	if len(sess.Context.CQs) > 0 {
		hints = append(hints, "能力问题：\n"+joinNumbered(sess.Context.CQs))
	}
	hints = append(hints, sess.Context.Hints...)
	res, err := e.LLM.Draft(sess.Context.Description, strings.Join(hints, "\n\n"))
	if err != nil && res == nil {
		return nil, err
	}
	raw, _ := json.Marshal(res.Spec)
	rm := json.RawMessage(raw)
	sess.Context.DraftSpec = &rm

	stage := "draft"
	var warn string
	if err != nil {
		warn = err.Error() // 达最大修正轮数仍有错误 → 草稿供预览参考
		stage = "refine"
	}
	reply := fmt.Sprintf("草稿已生成：概念 %d、关系 %d、实例 %d（生成-校验循环 %d 轮）。\n请在右侧预览确认：可直接入库，或回复修改意见进入修正轮。",
		len(res.Spec.Concepts), len(res.Spec.Relations), len(res.Spec.Instances), res.Rounds)
	if warn != "" {
		reply += "\n注意：" + warn
	}
	round := sess.Round + 1
	if err := st.Append(sess.ID, Message{Role: "assistant", Content: reply}, &stage, &round, &sess.Context); err != nil {
		return nil, err
	}
	return &TurnResult{Reply: reply, NextStage: stage, Draft: res.Spec, Warning: warn}, nil
}

// Refine 修正轮：用户修改意见 + 上稿校验错误回喂重新生成（由 turnDraft 复用：把意见并入 Hints 后再生成）。
func (e *Engine) Refine(st *Store, sess *Session, feedback string) (*TurnResult, error) {
	if fb := strings.TrimSpace(feedback); fb != "" {
		sess.Context.Hints = append(sess.Context.Hints, "修正意见："+fb)
	}
	return e.turnDraft(st, sess)
}

func isGenerateIntent(text string) bool {
	t := strings.TrimSpace(strings.ToLower(text))
	return t == "生成草稿" || t == "生成" || strings.HasPrefix(t, "生成草稿") ||
		t == "generate" || t == "draft"
}

func joinNumbered(items []string) string {
	parts := make([]string, len(items))
	for i, s := range items {
		parts[i] = fmt.Sprintf("%d. %s", i+1, s)
	}
	return strings.Join(parts, "\n")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
