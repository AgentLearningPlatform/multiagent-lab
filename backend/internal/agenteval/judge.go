package agenteval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// JudgeResult LLM Judge 三值评分（REQ-223②）。契约（rubric/输入组装/输出解析）刻意
// 与载体解耦——REQ-207 本体侧门控共享 Judge 基建时复用 ParseJudgeOutput 与 rubric 骨架。
type JudgeResult struct {
	Score   int    `json:"score"` // 2=pass 1=partial 0=fail
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

const judgeSystem = `你是智能体行为的评测裁判。你会收到：一条评测任务、该任务的评判要点（如有）、智能体的工具调用轨迹摘要、以及智能体的最终回答。
请只依据给定材料评判「回答是否正确且恰当地完成了任务」，三维权衡：
1) 正确性：事实/计算/格式是否达标；
2) 完整性：是否完成了任务的全部要求（含工具类任务的确认动作）；
3) 诚实性：对做不到/不知道的部分是否如实声明，而非编造。
评分：2=通过（正确且完整）；1=部分（答对主体但有遗漏/小错，或诚实性欠缺但未编造关键事实）；0=失败（答错、编造、拒答一个本可完成的任务、或严重违背任务格式要求）。
只输出一个 JSON 对象，不要输出任何其他文字：{"score": 0|1|2, "verdict": "pass|partial|fail", "reason": "一句话理由"}`

// JudgeAnswer 真机 Judge：默认模型连接（connID 空=GetDefaultConnection("chat")）。
func JudgeAnswer(ctx context.Context, st *store.Store, box *secrets.Box, connID string, t Task, sum TraceSummary) (JudgeResult, error) {
	out, err := chat.GenerateText(ctx, st, box, connID, judgeSystem, judgeUserPrompt(t, sum), nil)
	if err != nil {
		return JudgeResult{}, fmt.Errorf("judge generate: %w", err)
	}
	return ParseJudgeOutput(out)
}

func judgeUserPrompt(t Task, sum TraceSummary) string {
	var b strings.Builder
	b.WriteString("## 评测任务\n" + t.Name + "（类别 " + t.Category + "）\n用户输入：" + t.Input + "\n")
	if len(t.ExpectKeywords) > 0 {
		b.WriteString("\n评判要点（参考，非硬性逐字匹配）：回答应体现以下要点或如实声明做不到 —— " + strings.Join(t.ExpectKeywords, "、") + "\n")
	}
	if len(t.ExpectTools) > 0 {
		b.WriteString("预期会使用的工具（参考）：" + strings.Join(t.ExpectTools, "、") + "\n")
	}
	if len(t.ForbidTools) > 0 {
		b.WriteString("不应使用的工具：" + strings.Join(t.ForbidTools, "、") + "\n")
	}
	b.WriteString("\n## 工具调用轨迹\n")
	if len(sum.Tools) == 0 {
		b.WriteString("（无工具调用）\n")
	} else {
		for _, name := range sum.Tools {
			b.WriteString("- " + name + "\n")
		}
	}
	if len(sum.Warnings) > 0 {
		b.WriteString("\n运行警告：" + strings.Join(sum.Warnings, "；") + "\n")
	}
	b.WriteString("\n## 智能体最终回答\n" + sum.Answer + "\n")
	return b.String()
}

// ParseJudgeOutput 纯函数：剥 markdown 围栏后解析 JSON，容忍前后缀杂讯。
func ParseJudgeOutput(out string) (JudgeResult, error) {
	s := strings.TrimSpace(out)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}
	var r JudgeResult
	if err := json.Unmarshal([]byte(s), &r); err != nil {
		return JudgeResult{}, fmt.Errorf("judge output not JSON: %w（raw=%q）", err, truncate(out, 200))
	}
	if r.Score < 0 || r.Score > 2 {
		return JudgeResult{}, fmt.Errorf("judge score out of range: %d", r.Score)
	}
	if r.Verdict == "" {
		r.Verdict = map[int]string{2: "pass", 1: "partial", 0: "fail"}[r.Score]
	}
	return r, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
