package chat

import (
	"context"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// humanizeRunErr 截断类错误人话化（复杂问题答到一半即止的排障口径）：
// 工具参数流被 max_tokens 掐断 → eino 解析 eof，原始 NodeRunError 对用户不可读。
func TestHumanizeRunErr(t *testing.T) {
	cases := []struct {
		name   string
		msg    string
		finish string
		want   string // 期望包含的子串；空 = 原样返回
	}{
		{
			name: "工具参数解析失败且正常收尾（非截断，如实描述）",
			msg:  `[NodeRunError] failed to stream tool call call_x: [LocalFunc] failed to unmarshal arguments in json, toolName=save_file, err="Syntax error at index 9673: eof"`,
			want: "工具调用参数解析失败",
		},
		{
			name:   "参数解析失败 + finish=max_tokens → 按截断口径",
			msg:    `failed to unmarshal arguments in json, err="Syntax error at index 9673: eof"`,
			finish: "max_tokens",
			want:   "输出在达到 max_tokens 上限时被截断",
		},
		{
			name:   "finish_reason=max_tokens（Anthropic stop_reason 透传）",
			msg:    "some upstream error",
			finish: "max_tokens",
			want:   "输出在达到 max_tokens 上限时被截断",
		},
		{
			name:   "finish_reason=length",
			msg:    "some upstream error",
			finish: "length",
			want:   "输出在达到 max_tokens 上限时被截断",
		},
		{
			name: "普通错误原样保留",
			msg:  "connection refused",
			want: "",
		},
		{
			name: "原始错误附在提示末尾",
			msg:  `failed to unmarshal arguments in json, err="Syntax error at index 10: eof"`,
			want: "。原始错误：failed to unmarshal arguments",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := humanizeRunErr(c.msg, c.finish)
			if c.want == "" {
				if got != c.msg {
					t.Fatalf("普通错误应原样返回，得到 %q", got)
				}
				return
			}
			if !strings.Contains(got, c.want) {
				t.Fatalf("结果应包含 %q，得到 %q", c.want, got)
			}
		})
	}
}

// normalizeEmptyArgsTool 接口面保持断言：Invokable-only 包装不虚增 StreamableTool
// （compose ToolsNode 按断言优先走流式路径），流式工具包装后断言仍成立。
func TestNormalizeEmptyArgsToolInterface(t *testing.T) {
	invokable, _ := utils.InferTool("t1", "d", func(context.Context, struct{}) (string, error) { return "ok", nil })
	if _, ok := invokable.(einotool.StreamableTool); ok {
		t.Fatal("前置假设失效：InferTool 工具不应实现 StreamableTool")
	}
	w := normalizeEmptyArgsTool(invokable)
	if _, ok := w.(einotool.StreamableTool); ok {
		t.Error("Invokable-only 工具包装后不应变为 StreamableTool")
	}
	// 空参数归一化：空串 / 空白 → "{}"
	got, err := w.(einotool.InvokableTool).InvokableRun(context.Background(), "")
	if err != nil || got != "ok" {
		t.Errorf("空参数应归一化后执行成功，got=%q err=%v", got, err)
	}
	if _, err := w.(einotool.InvokableTool).InvokableRun(context.Background(), "  \n"); err != nil {
		t.Errorf("空白参数应归一化后执行成功: %v", err)
	}
	if normalizeEmptyArgsTool(w) != einotool.BaseTool(w) {
		t.Error("重复包裹应透传")
	}
}
