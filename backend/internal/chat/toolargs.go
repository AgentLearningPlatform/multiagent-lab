package chat

import (
	"context"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// 空参数归一化包装（2026-09-27 复杂问题答到一半即止排查的第二个根因）：
// GLM 等 Anthropic 兼容端点对空入参（{}）的 tool_use 不发 input_json_delta，eino 聚合出的
// Function.Arguments 为空串，utils.InferTool 解析空串报「input json is empty」并使整个 run
// 致命失败（NodeRunError）。空/纯空白参数统一改写为 "{}"——全可选字段的工具（如 list_files
// 缺省根目录）正常执行；缺必填字段的由工具自身参数校验报错回喂，不再炸 run。
// 包装严格保持原工具的接口面：Invokable-only 不虚增 StreamableTool（compose ToolsNode 按
// 断言优先走流式路径），MCP 等流式工具经 emptyArgsStreamTool 保能力不降级。

// normalizeEmptyArgsTool 包装单个工具；重复包裹透传，仅实现 BaseTool 的罕见形态原样返回。
func normalizeEmptyArgsTool(bt einotool.BaseTool) einotool.BaseTool {
	switch bt.(type) {
	case emptyArgsTool, emptyArgsStreamTool:
		return bt
	}
	if st, ok := bt.(einotool.StreamableTool); ok {
		return emptyArgsStreamTool{StreamableTool: st}
	}
	if it, ok := bt.(einotool.InvokableTool); ok {
		return emptyArgsTool{InvokableTool: it}
	}
	return bt
}

// normalizeToolArgs 空参数 → "{}"（eino 对空串直接解析报错，对 "{}" 得零值结构体）。
func normalizeToolArgs(args string) string {
	if strings.TrimSpace(args) == "" {
		return "{}"
	}
	return args
}

// emptyArgsTool Invokable-only 工具的空参数归一化包装。
type emptyArgsTool struct {
	einotool.InvokableTool
}

func (t emptyArgsTool) InvokableRun(ctx context.Context, argsInJSON string, opts ...einotool.Option) (string, error) {
	return t.InvokableTool.InvokableRun(ctx, normalizeToolArgs(argsInJSON), opts...)
}

// emptyArgsStreamTool 流式工具的空参数归一化包装（保持 StreamableTool 断言成立）。
type emptyArgsStreamTool struct {
	einotool.StreamableTool
}

func (t emptyArgsStreamTool) StreamableRun(ctx context.Context, argsInJSON string, opts ...einotool.Option) (*schema.StreamReader[string], error) {
	return t.StreamableTool.StreamableRun(ctx, normalizeToolArgs(argsInJSON), opts...)
}
