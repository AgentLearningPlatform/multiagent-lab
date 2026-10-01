package tool

// REQ-202/M38 Harness 执行面：工具调用 hooks（dsh tools/pre-execute|post-execute 的学习尺度形态）。
// 定位：审批（tool_approval）之外的确定性检查层——pre-hook 在工具执行前做守卫（拒绝=返回错误，
// 调用不执行、错误回喂模型），post-hook 观测结果。进程内 Go 注册表（守 D-O15，不引外部脚本运行时）。
// 纪律：hook 不改写参数与结果（dsh pre-execute 禁改 exec.arguments 同源——日志/UI/执行一致性）。
// 首批内置：fetchGuard（http_fetch 内网黑名单，双保险与工具内校验叠加）、cmdGuard（危险命令
// 模式表——当前无 run_command 工具，规则表先行，该类工具未来启用即受保护）。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// PreHookFn 执行前守卫函数：返回错误即拒绝本次调用（错误文本回喂模型）。
type PreHookFn func(toolName, argsJSON string) error

// PreHook 带 guard 名的守卫规格（REQ-224：hook.denied 审计事件载荷需要 guard 名）。
type PreHook struct {
	Guard string // 守卫名（fetchGuard / cmdGuard / 自定义）
	Fn    PreHookFn
}

// PostHook 执行后观测（只读；不改结果）。
type PostHook func(toolName, result string)

// HookChain hook 有序集合；Wrap 生成保持原工具接口面的包装。
type HookChain struct {
	Pre  []PreHook
	Post []PostHook
}

// Empty 是否无 hook（空链不包装，零开销）。
func (c *HookChain) Empty() bool { return c == nil || (len(c.Pre) == 0 && len(c.Post) == 0) }

// Wrap 包装单个工具：Invokable/Streamable 形态分别保持（与 toolargs.go 空参归一包装同纪律）。
func (c *HookChain) Wrap(bt einotool.BaseTool) einotool.BaseTool {
	if c.Empty() {
		return bt
	}
	if st, ok := bt.(einotool.StreamableTool); ok {
		return hookedStreamTool{StreamableTool: st, chain: c}
	}
	if it, ok := bt.(einotool.InvokableTool); ok {
		return hookedInvokableTool{InvokableTool: it, chain: c}
	}
	return bt
}

type hookedInvokableTool struct {
	einotool.InvokableTool
	chain *HookChain
}

func (h hookedInvokableTool) InvokableRun(ctx context.Context, args string, opts ...einotool.Option) (string, error) {
	name := toolNameOf(ctx, h.InvokableTool)
	for _, pre := range h.chain.Pre {
		if err := pre.Fn(name, args); err != nil {
			// 守卫拒绝 → 结构化拒绝文本回喂模型（对齐审批 deny 口径；dsh 纪律：调用失败不结束轮次）
			// REQ-224：hook.denied 结构化审计事件（guard/工具名/参数摘要/拒绝原因）
			EmitEvent(ctx, "hook.denied", map[string]any{
				"guard": pre.Guard, "tool_name": name,
				"args_digest": argsDigest(args), "reason": err.Error(),
			})
			b, _ := json.Marshal(map[string]string{"denied": err.Error()})
			return string(b), nil
		}
	}
	res, err := h.InvokableTool.InvokableRun(ctx, args, opts...)
	if err == nil {
		for _, post := range h.chain.Post {
			post(name, res)
		}
	}
	return res, err
}

// toolNameOf 取工具名（eino Info(ctx) 双返回值形态；失败回退空串——hook 按 name 过滤时跳过）。
func toolNameOf(ctx context.Context, it einotool.InvokableTool) string {
	info, err := it.Info(ctx)
	if err != nil || info == nil {
		return ""
	}
	return info.Name
}

type hookedStreamTool struct {
	einotool.StreamableTool
	chain *HookChain
}

func (h hookedStreamTool) StreamableRun(ctx context.Context, args string, opts ...einotool.Option) (*schema.StreamReader[string], error) {
	name := ""
	if info, ierr := h.StreamableTool.Info(ctx); ierr == nil && info != nil {
		name = info.Name
	}
	for _, pre := range h.chain.Pre {
		if err := pre.Fn(name, args); err != nil {
			EmitEvent(ctx, "hook.denied", map[string]any{
				"guard": pre.Guard, "tool_name": name,
				"args_digest": argsDigest(args), "reason": err.Error(),
			})
			return nil, err
		}
	}
	return h.StreamableTool.StreamableRun(ctx, args, opts...)
}

// ---- 内置 hooks ----

// NewFetchGuardPre http_fetch 守卫：从入参提取 URL 做内网/环回黑名单预检（与工具内校验双保险，
// 为审批前的快速拒绝点）。
func NewFetchGuardPre() PreHook {
	return PreHook{Guard: "fetchGuard", Fn: func(toolName, argsJSON string) error {
		if toolName != "http_fetch" {
			return nil
		}
		var in struct {
			URL string `json:"url"`
		}
		_ = json.Unmarshal([]byte(argsJSON), &in)
		u := strings.TrimSpace(in.URL)
		if u == "" {
			return fmt.Errorf("http_fetch 缺少 url")
		}
		if strings.Contains(u, "://localhost") || strings.Contains(u, "://127.") || strings.Contains(u, "://[::1]") ||
			strings.Contains(u, "://10.") || strings.Contains(u, "://192.168.") || strings.Contains(u, "://169.254.") {
			return fmt.Errorf("http_fetch 拒绝访问内网/环回地址（fetchGuard）")
		}
		return nil
	}}
}

// cmdGuardPatterns 危险命令模式表（run_command 类工具启用即生效；大小写不敏感子串匹配）。
var cmdGuardPatterns = []string{
	"rm -rf /", "rm -rf /*", "mkfs", ":(){ :", "fork bomb", "shutdown", "reboot",
	"dd if=", "> /dev/sd", "chmod -r 000 /", "mv /* ", "> /dev/null <", ": > /dev/sda",
}

// CommandGuard 危险命令守卫（pre-hook 形态；本期 run_command 未启用，机制与规则表先行）。
func CommandGuard(cmd string) (bool, string) {
	lc := strings.ToLower(cmd)
	for _, p := range cmdGuardPatterns {
		if strings.Contains(lc, p) {
			return false, "命令命中危险模式 " + p
		}
	}
	return true, ""
}

// argsDigest 审计载荷用参数摘要（≤200 字节 rune 安全截断——审计留痕不复制全文）。
func argsDigest(argsJSON string) string {
	r := []rune(strings.TrimSpace(argsJSON))
	if len(r) > 200 {
		return string(r[:200]) + "…"
	}
	return string(r)
}

// DefaultHookChain 装配期默认 hook 链（首批：fetchGuard；cmdGuard 随 run_command 工具接线）。
func DefaultHookChain() *HookChain {
	return &HookChain{Pre: []PreHook{NewFetchGuardPre()}}
}

// HookInfo hook 清单条目（REQ-231⑥：Harness 页签 hooks 卡数据源——前端静态文案退役，
// 由后端读取链真相；可配置化随 REQ-232）。
type HookInfo struct {
	Name        string `json:"name"`
	Active      bool   `json:"active"`
	Description string `json:"description"`
}

// DescribeHooks 当前 hook 注册真相（Active 自 DefaultHookChain 链长推导——fetchGuard 为
// 首个且当前唯一 pre-hook；cmdGuard 规则表先行、随 run_command 接线生效 REQ-225②）。
func DescribeHooks() []HookInfo {
	active := len(DefaultHookChain().Pre) > 0
	return []HookInfo{
		{Name: "fetchGuard", Active: active, Description: "http_fetch 执行前内网/环回地址预检（SSRF 粗防），拒绝时结构化回执回喂模型、调用不执行"},
		{Name: "cmdGuard", Active: false, Description: "危险命令模式表（随 run_command 工具接线生效，REQ-202 B1 三前置 / REQ-225②）"},
	}
}
