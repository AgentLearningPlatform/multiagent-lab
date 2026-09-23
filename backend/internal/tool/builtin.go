// 内置工具（方案 §6.8 工具注册表）：注册进 Registry 供 Agent 勾选装配。
package tool

import (
	"context"
	"fmt"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

type currentTimeIn struct {
	Timezone string `json:"timezone,omitempty" jsonschema_description:"IANA 时区名，如 Asia/Shanghai；缺省使用服务器本地时区"`
}

type currentTimeOut struct {
	Now      string `json:"now" jsonschema_description:"当前时间 YYYY-MM-DD HH:MM:SS"`
	Weekday  string `json:"weekday" jsonschema_description:"星期几（英文）"`
	Timezone string `json:"timezone" jsonschema_description:"实际使用的时区"`
}

func currentTimeFn(_ context.Context, in currentTimeIn) (*currentTimeOut, error) {
	loc := time.Local
	if in.Timezone != "" {
		l, err := time.LoadLocation(in.Timezone)
		if err != nil {
			return nil, fmt.Errorf("invalid timezone %q: %w", in.Timezone, err)
		}
		loc = l
	}
	n := time.Now().In(loc)
	return &currentTimeOut{
		Now:      n.Format("2006-01-02 15:04:05"),
		Weekday:  n.Weekday().String(),
		Timezone: loc.String(),
	}, nil
}

// RegisterBuiltin 注册全部内置工具。后续内置工具在此追加。
func RegisterBuiltin(r *Registry) error {
	bt, err := utils.InferTool("current_time",
		"获取当前日期时间与星期。当用户询问现在的时间/日期，或任务需要时间上下文时调用。",
		currentTimeFn)
	if err != nil {
		return fmt.Errorf("infer current_time tool: %w", err)
	}
	if err := r.Register(&Entry{
		ID:          "current_time",
		Description: "获取当前日期时间与星期（内置）",
		Source:      SourceBuiltin,
		New: func(ctx context.Context) (einotool.BaseTool, error) {
			return bt, nil
		},
	}); err != nil {
		return err
	}
	// M11 收尾：ask_human 人机协作中断恢复（勾选后 Agent 可挂起运行等待用户答复）
	ah, err := NewAskHumanTool()
	if err != nil {
		return fmt.Errorf("infer ask_human tool: %w", err)
	}
	return r.Register(&Entry{
		ID:          "ask_human",
		Description: "向用户提问并等待答复（中断恢复，内置）",
		Source:      SourceBuiltin,
		New: func(ctx context.Context) (einotool.BaseTool, error) {
			return ah, nil
		},
	})
}
