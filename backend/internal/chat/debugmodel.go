// REQ-117 / M17 执行调试模式：模型调用链路采集器（装饰器）。
// debugModel 包装 BaseChatModel，在每次 Generate/Stream 时按观测级别发 model.step 事件：
//
//	level 0 简洁：不包装，零开销；
//	level 1 详细：摘要（agent/轮次/耗时/usage/finish/输入规模/绑定工具名）；
//	level 2 调试：另附完整输入 messages 与工具 schema（含描述）。
//
// model.step 仅实时透传（不落 run_events 表，控制存储膨胀）；历史回放暂不含该事件（阶段二入库开关）。
package chat

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// DebugLevel 观测级别（REQ-117）：0 简洁 / 1 详细 / 2 调试。
type DebugLevel int

const (
	DebugOff      DebugLevel = 0
	DebugDetailed DebugLevel = 1
	DebugFull     DebugLevel = 2
)

type debugRecorder struct {
	level  DebugLevel
	runID  string
	emit   EmitFn       // 实时透传
	record func(*Event) // M17 阶段二：入库开关开启时同步落 run_events（nil = 仅透传）
	seq    atomic.Int64
}

type debugCtxKey struct{}

// withDebug 注入采集器（level>0 才注入；Run/Resume 在装配前调用，装饰器在调用期读取）。
func withDebug(ctx context.Context, rec *debugRecorder) context.Context {
	if rec == nil {
		return ctx
	}
	return context.WithValue(ctx, debugCtxKey{}, rec)
}

func debugFrom(ctx context.Context) *debugRecorder {
	rec, _ := ctx.Value(debugCtxKey{}).(*debugRecorder)
	return rec
}

// step 输出一次模型调用事件（M17 阶段二：persist 开启时同步落库，否则仅透传）。
func (d *debugRecorder) step(agent string, dur time.Duration, input []*schema.Message, tools []*schema.ToolInfo, usage *schema.TokenUsage, finish string, runErr string) {
	if d == nil || (d.emit == nil && d.record == nil) {
		return
	}
	seq := d.seq.Add(1)
	data := map[string]any{
		"agent":       agent,
		"seq":         seq,
		"duration_ms": dur.Milliseconds(),
		"input_chars": sumChars(input),
		"input_count": len(input),
	}
	if usage != nil {
		data["usage"] = map[string]any{
			"prompt_tokens":     usage.PromptTokens,
			"completion_tokens": usage.CompletionTokens,
			"total_tokens":      usage.TotalTokens,
		}
	}
	if finish != "" {
		data["finish_reason"] = finish
	}
	if runErr != "" {
		data["error"] = runErr
	}
	roles := make([]map[string]any, 0, len(input))
	for _, m := range input {
		entry := map[string]any{"role": string(m.Role), "chars": len(m.Content)}
		if d.level >= DebugFull {
			c := m.Content
			if len(c) > 4000 {
				c = c[:4000] + "…(截断)"
			}
			entry["content"] = c
		} else if len(m.Content) > 80 {
			entry["preview"] = m.Content[:80] + "…"
		} else {
			entry["preview"] = m.Content
		}
		roles = append(roles, entry)
	}
	data["input"] = roles
	if len(tools) > 0 {
		toolInfos := make([]map[string]any, 0, len(tools))
		for _, ti := range tools {
			if ti == nil {
				continue
			}
			entry := map[string]any{"name": ti.Name}
			if d.level >= DebugFull && ti.Desc != "" {
				desc := ti.Desc
				if len(desc) > 400 {
					desc = desc[:400] + "…(截断)"
				}
				entry["desc"] = desc
			}
			toolInfos = append(toolInfos, entry)
		}
		data["tools"] = toolInfos
	}
	ev := newEvent("model.step", d.runID, data)
	if d.record != nil {
		// 入库开关开启：emitAndRecord 同时承担实时透传与落库（此前再走 d.emit 会致 SSE 重复发送同一事件，
		// REQ-149 核验时发现修复）；未开启：仅实时透传
		d.record(ev)
	} else if d.emit != nil {
		d.emit(ev)
	}
}

func sumChars(input []*schema.Message) int {
	n := 0
	for _, m := range input {
		n += len(m.Content)
	}
	return n
}

// debugModel 装饰器：透传 Generate/Stream 并按级别采集 model.step。
// 工具由 ADK 经 model.WithTools 请求选项绑定（ChatModelAgent 语义），故从 opts 读取本次绑定的工具。
type debugModel struct {
	inner model.BaseChatModel
	agent string
	rec   *debugRecorder
}

func (m *debugModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	start := time.Now()
	out, err := m.inner.Generate(ctx, input, opts...)
	m.report(ctx, start, input, opts, out, err)
	return out, err
}

func (m *debugModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	start := time.Now()
	sr, err := m.inner.Stream(ctx, input, opts...)
	if err != nil {
		m.report(ctx, start, input, opts, nil, err)
		return nil, err
	}
	// eino 官方 tee 模式：复制为两路——一路返回给调用方，一路旁路捕获 ResponseMeta（usage/finish 在尾部 chunk）
	copies := sr.Copy(2)
	outSr := copies[0]
	go func() {
		defer copies[1].Close()
		var usage *schema.TokenUsage
		finish := ""
		for {
			chunk, rerr := copies[1].Recv()
			if rerr != nil {
				runErr := ""
				if !errors.Is(rerr, io.EOF) {
					runErr = rerr.Error()
				}
				m.reportWith(ctx, start, input, opts, usage, finish, runErr)
				return
			}
			if chunk != nil && chunk.ResponseMeta != nil {
				if chunk.ResponseMeta.Usage != nil {
					usage = chunk.ResponseMeta.Usage
				}
				if chunk.ResponseMeta.FinishReason != "" {
					finish = chunk.ResponseMeta.FinishReason
				}
			}
		}
	}()
	return outSr, nil
}

func (m *debugModel) report(ctx context.Context, start time.Time, input []*schema.Message, opts []model.Option, out *schema.Message, callErr error) {
	var usage *schema.TokenUsage
	finish := ""
	if out != nil && out.ResponseMeta != nil {
		usage = out.ResponseMeta.Usage
		finish = out.ResponseMeta.FinishReason
	}
	m.reportWith(ctx, start, input, opts, usage, finish, errText(callErr))
}

func (m *debugModel) reportWith(ctx context.Context, start time.Time, input []*schema.Message, opts []model.Option, usage *schema.TokenUsage, finish, runErr string) {
	if m.rec == nil {
		return
	}
	tools := toolInfosFromOpts(opts)
	m.rec.step(m.agent, time.Since(start), input, tools, usage, finish, runErr)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > 400 {
		s = s[:400] + "…(截断)"
	}
	return s
}

func toolInfosFromOpts(opts []model.Option) []*schema.ToolInfo {
	if len(opts) == 0 {
		return nil
	}
	o := model.GetCommonOptions(&model.Options{}, opts...)
	return o.Tools
}

// wrapDebug 按观测级别包装模型（level 0 原样返回）。
func wrapDebug(inner model.BaseChatModel, agent string, rec *debugRecorder) model.BaseChatModel {
	if rec == nil || rec.level <= DebugOff || inner == nil {
		return inner
	}
	return &debugModel{inner: inner, agent: agent, rec: rec}
}
