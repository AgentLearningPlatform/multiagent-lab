package agenteval

import (
	"fmt"
	"strings"
)

// EventRecord 一条 run 事件（SSE 信封解出后的投影——跑分器只关心这几类）。
type EventRecord struct {
	Type string
	Data map[string]any
}

// TraceSummary 从事件流提取的轨迹摘要（Answer 来自 live 流的 message.delta——
// message.delta 不落库是已知口径，HTTP 跑分器走实时 SSE 因此可见）。
type TraceSummary struct {
	Answer       string
	Tools        []string // tool.call 的 tool_name 按序
	ToolCount    int
	Warnings     []string
	Finished     bool
	FinishReason string
	ErrMsg       string
	TotalTokens  int
}

// SummarizeEvents 纯函数：事件流 → 轨迹摘要。
func SummarizeEvents(events []EventRecord) TraceSummary {
	var sum TraceSummary
	var sb strings.Builder
	seenTool := map[string]bool{}
	for _, ev := range events {
		switch ev.Type {
		case "message.delta":
			if d, ok := ev.Data["delta"].(string); ok {
				sb.WriteString(d)
			}
		case "tool.call":
			name, _ := ev.Data["tool_name"].(string)
			if name != "" {
				sum.Tools = append(sum.Tools, name)
				seenTool[name] = true
			}
		case "run.warning":
			msg, _ := ev.Data["message"].(string)
			if msg != "" {
				sum.Warnings = append(sum.Warnings, msg)
			}
		case "run.finished":
			sum.Finished = true
			sum.FinishReason, _ = ev.Data["reason"].(string)
			if usage, ok := ev.Data["usage"].(map[string]any); ok {
				if t, ok := usage["total_tokens"].(float64); ok {
					sum.TotalTokens = int(t)
				}
			}
		case "run.error":
			sum.ErrMsg, _ = ev.Data["message"].(string)
		}
	}
	sum.Answer = sb.String()
	sum.ToolCount = len(sum.Tools)
	_ = seenTool
	return sum
}

// Finding 单条轨迹断言结果。
type Finding struct {
	OK     bool
	Kind   string // expect_tool | forbid_tool | finished | no_error | warnings | tokens
	Detail string
}

// CheckTrace 纯函数：轨迹摘要 vs 任务要求 → 断言列表。
func CheckTrace(sum TraceSummary, t Task) []Finding {
	var out []Finding
	if len(t.ExpectTools) > 0 {
		hit := ""
		for _, want := range t.ExpectTools {
			for _, got := range sum.Tools {
				if got == want {
					hit = want
					break
				}
			}
			if hit != "" {
				break
			}
		}
		out = append(out, Finding{OK: hit != "", Kind: "expect_tool", Detail: fmt.Sprintf("期望工具任一命中 %v：命中=%q（实际调用 %v）", t.ExpectTools, hit, sum.Tools)})
	}
	for _, bad := range t.ForbidTools {
		hit := false
		for _, got := range sum.Tools {
			if got == bad {
				hit = true
				break
			}
		}
		out = append(out, Finding{OK: !hit, Kind: "forbid_tool", Detail: fmt.Sprintf("禁用工具 %s：出现=%v", bad, hit)})
	}
	out = append(out, Finding{OK: sum.Finished, Kind: "finished", Detail: "run 正常收尾=" + fmt.Sprint(sum.Finished) + " reason=" + sum.FinishReason})
	out = append(out, Finding{OK: sum.ErrMsg == "", Kind: "no_error", Detail: "run.error=" + sum.ErrMsg})
	if t.MaxWarnings > 0 && len(sum.Warnings) > t.MaxWarnings {
		out = append(out, Finding{OK: false, Kind: "warnings", Detail: fmt.Sprintf("警告数 %d 超上限 %d：%v", len(sum.Warnings), t.MaxWarnings, sum.Warnings)})
	}
	if t.MaxTotalTokens > 0 && sum.TotalTokens > t.MaxTotalTokens {
		out = append(out, Finding{OK: false, Kind: "tokens", Detail: fmt.Sprintf("token %d 超上限 %d", sum.TotalTokens, t.MaxTotalTokens)})
	}
	return out
}

// TracePassed 全部断言通过。
func TracePassed(findings []Finding) bool {
	for _, f := range findings {
		if !f.OK {
			return false
		}
	}
	return true
}
