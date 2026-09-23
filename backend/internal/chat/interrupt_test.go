// 中断恢复全链路单测（M11 收尾）：stub 模型驱动 ask_human → 中断存档 → 定向恢复 → 续跑完成。
// 验证：ask_human 工具中断 / run.interrupted 信息提取 / checkpoint gob / ResumeWithParams 地址定向 / 工具重入取答复。
package chat

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/tool"
)

// stubAskModel 桩模型：首轮发起 ask_human 工具调用；收到 tool 结果（用户答复）后输出最终回答。
type stubAskModel struct{}

func (stubAskModel) Generate(_ context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	for i := len(input) - 1; i >= 0; i-- {
		if input[i].Role == schema.Tool {
			return schema.AssistantMessage("已确认："+input[i].Content, nil), nil
		}
	}
	return schema.AssistantMessage("", []schema.ToolCall{
		{ID: "call_1", Function: schema.FunctionCall{Name: "ask_human", Arguments: `{"question":"输出语言用哪种？","choices":["中文","English"]}`}},
	}), nil
}

func (stubAskModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("stub model: stream not implemented")
}

// consumeEvents 消费事件流，返回根因中断点（无中断则 nil）与最终文本。
func consumeEvents(t *testing.T, iter *adk.AsyncIterator[*adk.AgentEvent]) (root *adk.InterruptCtx, text string) {
	t.Helper()
	for {
		ev, ok := iter.Next()
		if !ok {
			return root, text
		}
		if ev.Err != nil {
			t.Fatalf("event error: %v", ev.Err)
		}
		if ev.Action != nil && ev.Action.Interrupted != nil {
			for _, c := range ev.Action.Interrupted.InterruptContexts {
				if c == nil {
					continue
				}
				if root == nil || (c.IsRootCause && !root.IsRootCause) {
					root = c
				}
			}
		}
		if ev.Output != nil && ev.Output.MessageOutput != nil && ev.Output.MessageOutput.Message != nil {
			mo := ev.Output.MessageOutput
			if mo.Role == schema.Assistant && mo.Message.Content != "" {
				text = mo.Message.Content
			}
		}
	}
}

func TestAskHumanInterruptResume(t *testing.T) {
	ctx := context.Background()
	ah, err := tool.NewAskHumanTool()
	if err != nil {
		t.Fatal(err)
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "tester",
		Instruction: "需要用户输入时调用 ask_human。",
		Model:       stubAskModel{},
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: []einotool.BaseTool{ah}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := NewMemCheckPointStore()
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: false, CheckPointStore: store})

	// 1) 首轮运行：中断并拿到问题
	iter := runner.Run(ctx, []*schema.Message{schema.UserMessage("请问我一个问题")}, adk.WithCheckPointID("ckpt_t1"))
	root, text := consumeEvents(t, iter)
	if root == nil {
		t.Fatalf("expected interrupt, got none (last text=%q)", text)
	}
	info, ok := root.Info.(*tool.AskHumanInfo)
	if !ok {
		t.Fatalf("interrupt info type = %T, want *tool.AskHumanInfo", root.Info)
	}
	if info.Question != "输出语言用哪种？" || len(info.Choices) != 2 {
		t.Fatalf("interrupt info = %+v", info)
	}
	if root.ID == "" {
		t.Fatal("interrupt ctx ID empty (resume target key)")
	}

	// 2) 定向恢复：答复"中文"→ 工具重入取答复 → 模型输出最终回答
	iter2, err := runner.ResumeWithParams(ctx, "ckpt_t1", &adk.ResumeParams{Targets: map[string]any{root.ID: "中文"}})
	if err != nil {
		t.Fatal(err)
	}
	root2, text2 := consumeEvents(t, iter2)
	if root2 != nil {
		t.Fatalf("unexpected second interrupt: %+v", root2.Info)
	}
	if !strings.Contains(text2, "已确认") || !strings.Contains(text2, "中文") {
		t.Fatalf("final text = %q, want 已确认 + 用户答复", text2)
	}
}
