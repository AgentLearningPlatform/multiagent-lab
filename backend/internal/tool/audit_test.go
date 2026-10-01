package tool

import (
	"context"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// REQ-224/M52：hook.denied 结构化审计事件（guard 名/参数摘要/原因经事件汇发出）。
func TestHookDeniedEvent(t *testing.T) {
	var got []map[string]any
	ctx := WithEventSink(context.Background(), func(eventType string, data map[string]any) {
		if eventType == "hook.denied" {
			got = append(got, data)
		}
	})
	chain := DefaultHookChain()
	inner := &fakeInvokable{}
	wrapped := chain.Wrap(inner)
	it, ok := wrapped.(einotool.InvokableTool)
	if !ok {
		t.Fatalf("应保持 InvokableTool 接口")
	}
	res, err := it.InvokableRun(ctx, `{"url":"http://192.168.1.5/x"}`)
	if err != nil {
		t.Fatalf("守卫拒绝应回执不报错: %v", err)
	}
	if res == "" {
		t.Fatalf("应有拒绝回执")
	}
	if len(got) != 1 {
		t.Fatalf("应恰好一条 hook.denied，got %d", len(got))
	}
	if got[0]["guard"] != "fetchGuard" || got[0]["tool_name"] != "http_fetch" {
		t.Fatalf("载荷缺 guard/tool_name: %+v", got[0])
	}
	if s, _ := got[0]["args_digest"].(string); s == "" {
		t.Fatalf("载荷缺 args_digest: %+v", got[0])
	}
	// 无汇（非运行期）不 panic
	_, _ = it.InvokableRun(context.Background(), `{"url":"http://192.168.1.5/x"}`)
}

type fakeInvokable struct{}

func (f *fakeInvokable) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "http_fetch"}, nil
}

func (f *fakeInvokable) InvokableRun(_ context.Context, _ string, _ ...einotool.Option) (string, error) {
	return "should-not-run", nil
}
