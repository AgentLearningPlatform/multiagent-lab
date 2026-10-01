package agenteval

import (
	"strings"
	"testing"
)

func mkEv(t string, kv ...any) EventRecord {
	d := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		d[kv[i].(string)] = kv[i+1]
	}
	return EventRecord{Type: t, Data: d}
}

func TestSummarizeEvents(t *testing.T) {
	events := []EventRecord{
		mkEv("run.started", "agent_name", "a"),
		mkEv("message.delta", "delta", "你好"),
		mkEv("message.delta", "delta", "，世界"),
		mkEv("tool.call", "tool_name", "current_time"),
		mkEv("tool.result", "tool_name", "current_time", "duration_ms", 12.0),
		mkEv("tool.call", "tool_name", "write_file"),
		mkEv("run.warning", "message", "上下文裁剪"),
		mkEv("run.finished", "reason", "completed", "usage", map[string]any{"total_tokens": 1234.0}),
	}
	sum := SummarizeEvents(events)
	if sum.Answer != "你好，世界" {
		t.Fatalf("answer=%q", sum.Answer)
	}
	if len(sum.Tools) != 2 || sum.Tools[0] != "current_time" || sum.Tools[1] != "write_file" {
		t.Fatalf("tools=%v", sum.Tools)
	}
	if len(sum.Warnings) != 1 {
		t.Fatalf("warnings=%v", sum.Warnings)
	}
	if !sum.Finished || sum.FinishReason != "completed" || sum.TotalTokens != 1234 {
		t.Fatalf("finished=%v reason=%s tokens=%d", sum.Finished, sum.FinishReason, sum.TotalTokens)
	}
}

func TestCheckTrace(t *testing.T) {
	sum := TraceSummary{
		Tools: []string{"current_time"}, Finished: true, FinishReason: "completed",
		Warnings: []string{"w1", "w2", "w3", "w4"}, TotalTokens: 500,
	}
	task := Task{ID: "x", ExpectTools: []string{"current_time", "read_file"}, ForbidTools: []string{"run_command"}, MaxWarnings: 3, MaxTotalTokens: 400}
	fs := CheckTrace(sum, task)
	byKind := map[string][]Finding{}
	for _, f := range fs {
		byKind[f.Kind] = append(byKind[f.Kind], f)
	}
	// expect_tool OR 语义：current_time 命中即整组 OK（read_file 未命中不影响）
	if len(byKind["expect_tool"]) != 1 {
		t.Fatalf("expect_tool findings=%v", byKind["expect_tool"])
	}
	if !byKind["expect_tool"][0].OK {
		t.Fatalf("expect_tool 任一命中应 OK: %+v", byKind["expect_tool"])
	}
	// 无一命中 → FAIL
	fs2 := CheckTrace(TraceSummary{Finished: true}, Task{ID: "y", ExpectTools: []string{"grep", "glob"}})
	if fs2[0].OK {
		t.Fatalf("期望工具无一命中应 FAIL")
	}
	if !byKind["forbid_tool"][0].OK {
		t.Fatalf("forbid_tool 未出现应 OK")
	}
	if byKind["finished"][0].OK == false || byKind["no_error"][0].OK == false {
		t.Fatalf("finished/no_error 应 OK")
	}
	if byKind["warnings"][0].OK {
		t.Fatalf("warnings 4>3 应 FAIL")
	}
	if byKind["tokens"][0].OK {
		t.Fatalf("tokens 500>400 应 FAIL")
	}
	if TracePassed(fs) {
		t.Fatalf("存在 FAIL 不应整体通过")
	}
	if !TracePassed(CheckTrace(TraceSummary{Finished: true}, Task{})) {
		t.Fatalf("空任务应通过")
	}
}

func TestParseJudgeOutput(t *testing.T) {
	cases := []struct {
		in      string
		score   int
		verdict string
		ok      bool
	}{
		{`{"score":2,"verdict":"pass","reason":"ok"}`, 2, "pass", true},
		{"```json\n{\"score\":1,\"verdict\":\"partial\",\"reason\":\"基本对\"}\n```", 1, "partial", true},
		{"裁判认为：{\"score\":0,\"verdict\":\"fail\",\"reason\":\"编造了事实\"} 以上。", 0, "fail", true},
		{`{"score":2,"reason":"只给分数"}`, 2, "pass", true}, // verdict 缺省补齐
		{`{"score":5}`, 0, "", false}, // 越界
		{`完全不是 JSON`, 0, "", false},   // 垃圾输出
	}
	for i, c := range cases {
		got, err := ParseJudgeOutput(c.in)
		if c.ok {
			if err != nil {
				t.Fatalf("case %d: %v", i, err)
			}
			if got.Score != c.score || got.Verdict != c.verdict {
				t.Fatalf("case %d: got %+v", i, got)
			}
		} else if err == nil {
			t.Fatalf("case %d: 应解析失败", i)
		}
	}
	if !strings.Contains(judgeSystem, "2=通过") {
		t.Fatalf("rubric 骨架不应被改动")
	}
}
