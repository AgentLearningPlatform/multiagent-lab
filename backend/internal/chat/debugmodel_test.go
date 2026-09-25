package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type fakeInner struct{ calls int }

func (f *fakeInner) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	f.calls++
	return schema.AssistantMessage("ok", nil), nil
}
func (f *fakeInner) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	f.calls++
	msg := schema.AssistantMessage("stream-ok", nil)
	msg.ResponseMeta = &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: 210, CompletionTokens: 12, TotalTokens: 222}, FinishReason: "stop"}
	sr, sw := schema.Pipe[*schema.Message](2)
	_ = sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

func TestDebugModelEmitsStep(t *testing.T) {
	var got []map[string]any
	rec := &debugRecorder{level: DebugFull, runID: "r1", emit: func(ev *Event) {
		var m map[string]any
		_ = json.Unmarshal(ev.Data, &m)
		got = append(got, m)
	}}
	inner := &fakeInner{}
	m := wrapDebug(inner, "测试Agent", rec)
	if m == inner {
		t.Fatal("expected wrapped model")
	}
	sr, err := m.Stream(context.Background(), []*schema.Message{schema.UserMessage("你好")}, model.WithTools([]*schema.ToolInfo{{Name: "current_time", Desc: "查时间"}}))
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, e := sr.Recv(); e != nil {
			break
		}
	}
	time.Sleep(200 * time.Millisecond) // 旁路 goroutine 的上报与消费端 EOF 存在天然竞态
	if len(got) != 1 {
		t.Fatalf("model.step events = %d, want 1", len(got))
	}
	d := got[0]
	if d["agent"] != "测试Agent" || d["finish_reason"] != "stop" {
		t.Fatalf("unexpected step data: %v", d)
	}
	if u, ok := d["usage"].(map[string]any); !ok || u["total_tokens"] != float64(222) {
		t.Fatalf("usage missing: %v", d["usage"])
	}
	raw, _ := json.Marshal(d)
	joined := string(raw)
	for _, want := range []string{`"current_time"`, `"查时间"`, `"finish_reason":"stop"`, `"total_tokens":222`, `"role":"user"`, `"content":"你好"`} {
		if !strings.Contains(joined, want) {
			t.Fatalf("step data missing %s in: %s", want, joined)
		}
	}
}
