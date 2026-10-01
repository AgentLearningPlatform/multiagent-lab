package tool

import "context"

// REQ-224/M52：运行期事件汇（结构化审计事件的载体）。
// runner 在 Run/Resume 装配前把「写 run_event 的回调」放进 ctx；装配层与工具包装层
// （审批自动拒/守卫拒绝/连接器降级）经 EmitEvent 发出领域事件——SSE 与落库同源，
// 不再有「从 tool.result 文本反推审计」的缺口。tool 包被 chat 导入，定义在此避免环。

// EventSink 运行期事件接收器（非阻塞；nil 安全）。
type EventSink func(eventType string, data map[string]any)

type sinkKey struct{}

// WithEventSink 在 ctx 上挂事件汇（runner 专用写入点）。
func WithEventSink(ctx context.Context, sink EventSink) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, sinkKey{}, sink)
}

// EmitEvent 经 ctx 事件汇发事件；无汇（非运行期调用/单测）时静默跳过。
func EmitEvent(ctx context.Context, eventType string, data map[string]any) {
	if ctx == nil {
		return
	}
	sink, _ := ctx.Value(sinkKey{}).(EventSink)
	if sink != nil {
		sink(eventType, data)
	}
}
