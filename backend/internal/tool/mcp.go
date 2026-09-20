package tool

import (
	"context"
	"fmt"
	"time"

	mcpp "github.com/cloudwego/eino-ext/components/tool/mcp"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// renamedInvokable 改写暴露给模型的工具名并保留 Invoke 能力（§6.11）。
type renamedInvokable struct {
	einotool.InvokableTool
	name string
}

func (r *renamedInvokable) Info(ctx context.Context) (*schema.ToolInfo, error) {
	ti, err := r.InvokableTool.Info(ctx)
	if err != nil {
		return nil, err
	}
	c := *ti // 浅拷贝，避免改写共享 ToolInfo
	c.Name = r.name
	return &c, nil
}

// renamedStreamable 改写暴露给模型的流式工具名并保留 StreamInvoke 能力。
type renamedStreamable struct {
	einotool.StreamableTool
	name string
}

func (r *renamedStreamable) Info(ctx context.Context) (*schema.ToolInfo, error) {
	ti, err := r.StreamableTool.Info(ctx)
	if err != nil {
		return nil, err
	}
	c := *ti
	c.Name = r.name
	return &c, nil
}

// FetchMCPTools 连接 streamable HTTP MCP server 并拉取全部工具（§6.11）。
// 工具以 {server}__{tool} 前缀重命名后返回；连接/初始化/列工具失败返回 error，
// 由装配层降级记告警（§13 风险：MCP 不可用不阻断运行，不做重试风暴）。
func FetchMCPTools(ctx context.Context, serverName, url string, timeout time.Duration) ([]einotool.BaseTool, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cli, err := mcpclient.NewStreamableHttpClient(url)
	if err != nil {
		return nil, fmt.Errorf("mcp client create: %w", err)
	}
	if err := cli.Start(cctx); err != nil {
		return nil, fmt.Errorf("mcp start: %w", err)
	}
	if _, err := cli.Initialize(cctx, mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: "2025-03-26",
			ClientInfo:      mcp.Implementation{Name: "eino-multiagent-lab", Version: "0.1.0"},
		},
	}); err != nil {
		return nil, fmt.Errorf("mcp initialize: %w", err)
	}

	bts, err := mcpp.GetTools(cctx, &mcpp.Config{Cli: cli})
	if err != nil {
		return nil, fmt.Errorf("mcp list tools: %w", err)
	}

	out := make([]einotool.BaseTool, 0, len(bts))
	prefix := serverName + "__"
	for _, bt := range bts {
		ti, err := bt.Info(ctx)
		if err != nil || ti == nil || ti.Name == "" {
			continue
		}
		// 保留底层工具的调用能力：按 Invokable / Streamable 分别包装，仅改写名字。
		switch t := bt.(type) {
		case einotool.InvokableTool:
			out = append(out, &renamedInvokable{InvokableTool: t, name: prefix + ti.Name})
		case einotool.StreamableTool:
			out = append(out, &renamedStreamable{StreamableTool: t, name: prefix + ti.Name})
		default:
			out = append(out, bt)
		}
	}
	return out, nil
}
