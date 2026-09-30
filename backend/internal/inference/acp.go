// S2/REQ-206（M41）：ACP 主通道客户端——dsh --profile acp 的 JSON-RPC 2.0 over stdio 客户端
// 与 acpAdapter 装配。
//
// 协议帧形态以 31 号 §12 S0 实证为准（smoke/dsh-s0 采集）：
//   - initialize{protocolVersion,clientCapabilities} → agentCapabilities（含 sessionCapabilities.resume、
//     mcpCapabilities.http）；
//   - session/new{cwd,mcpServers} → {sessionId}（sessionId 客户端自由命名亦可，平台生成 plt- 前缀）；
//   - session/prompt{sessionId,content} → **turn 结束时才应答**（stopReason: end_turn/cancelled…），
//     过程事件经 session/update 通知流出（agent_message_chunk / tool_call{rawInput} →
//     tool_call_update{completed|failed}）；
//   - session/cancel{sessionId} 通知 = 中断；session/request_permission 请求 → **默认 deny**（可编程
//     应答，S0 设计输入；审批策略语义真机联调后再升级为转审批）；
//   - session/resume{sessionId,configOptions} → 跨进程恢复会话（M24 存量会话实证可恢复）。
//
// 诚实边界：本机未装 dsh 二进制——协议机器面以 stub ACP agent（Node 按上述帧形态应答）验证；
// 真机 dsh 全链（真实模型轮/权限转审批/MCP http 挂载翻正/会话延续语义）待装机联调。
package inference

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

// acpSession 一次长驻 dsh 进程上的会话客户端（JSON-RPC 2.0 行分帧）。
type acpSession struct {
	proc  *streamingProc
	mu    sync.Mutex
	next  int64
	pend  map[int64]chan acpResp
	notif func(method string, params json.RawMessage)
}

type acpResp struct {
	result json.RawMessage
	errMsg string
}

func newACPSession(bin string, args []string, cwd string, notif func(string, json.RawMessage)) (*acpSession, error) {
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("PATH 中未找到 %q（未安装或未加入 PATH）", bin)
	}
	s := &acpSession{pend: map[int64]chan acpResp{}, notif: notif}
	proc, err := startStreaming("dsh-acp", path, args, cwd, os.Environ(), func(line string) {
		s.onFrame(line)
	})
	if err != nil {
		return nil, err
	}
	s.proc = proc
	return s, nil
}

// onFrame 行帧分发：JSON-RPC response（带 id）→ 唤醒等待者；notification/request → notif 回调。
// （session/request_permission 以 server→client request 形态到达，也走 notif，由适配器应答。）
func (s *acpSession) onFrame(line string) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] != '{' {
		return
	}
	var frame struct {
		ID     *int64          `json:"id"`
		Method string          `json:"method"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
		Params json.RawMessage `json:"params"`
	}
	if json.Unmarshal([]byte(line), &frame) != nil {
		return
	}
	if frame.ID != nil && frame.Method != "" {
		// server→client request（如 session/request_permission）：交 notif 由适配器应答
		if s.notif != nil {
			s.notif(frame.Method, frame.Params)
		}
		return
	}
	if frame.ID != nil {
		s.mu.Lock()
		ch, ok := s.pend[*frame.ID]
		if ok {
			delete(s.pend, *frame.ID)
		}
		s.mu.Unlock()
		if ok {
			msg := ""
			if frame.Error != nil {
				msg = frame.Error.Message
			}
			ch <- acpResp{result: frame.Result, errMsg: msg}
		}
		return
	}
	if frame.Method != "" && s.notif != nil {
		s.notif(frame.Method, frame.Params)
	}
}

// call 发送 JSON-RPC 请求并等待应答（timeout 兜底防挂死）。
func (s *acpSession) call(ctx context.Context, method string, params any, timeout time.Duration) (json.RawMessage, error) {
	s.mu.Lock()
	s.next++
	id := s.next
	ch := make(chan acpResp, 1)
	s.pend[id] = ch
	s.mu.Unlock()
	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if err := s.proc.send(string(req)); err != nil {
		return nil, err
	}
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case r := <-ch:
		if r.errMsg != "" {
			return nil, fmt.Errorf("%s 应答错误: %s", method, r.errMsg)
		}
		return r.result, nil
	case <-t.C:
		s.mu.Lock()
		delete(s.pend, id)
		s.mu.Unlock()
		return nil, fmt.Errorf("%s 超时（%s）", method, timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// notify 发送通知（无 id，不应答）。
func (s *acpSession) notify(method string, params any) error {
	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	return s.proc.send(string(req))
}

// respond 应答 server→client 的 request（如 session/request_permission）。
func (s *acpSession) respond(id int64, result any) error {
	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	if err != nil {
		return err
	}
	return s.proc.send(string(req))
}

func (s *acpSession) close() { s.proc.close() }

// onUpdate session/update 事件转译（S0 帧形态）：agent_message_chunk→message.delta、
// tool_call（含 rawInput）→tool.call、tool_call_update（completed/failed+output.content）→tool.result。
func (s *acpSession) onUpdate(params json.RawMessage, emit func(Event)) {
	var u acpUpdate
	if json.Unmarshal(params, &u) != nil {
		return
	}
	switch u.Update.SessionUpdate {
	case "agent_message_chunk":
		if u.Update.Content != nil && u.Update.Content.Text != "" {
			emit(Event{Type: "message.delta", Data: map[string]any{"delta": u.Update.Content.Text}})
		}
	case "tool_call":
		data := map[string]any{"name": u.Update.Title, "source": "inference"}
		if len(u.Update.RawInput) > 0 {
			var input map[string]any
			if json.Unmarshal(u.Update.RawInput, &input) == nil {
				data["args"] = input
			}
		}
		emit(Event{Type: "tool.call", Data: data})
	case "tool_call_update":
		text := ""
		if u.Update.Output != nil {
			for _, c := range u.Update.Output.Content {
				if c.Text != "" {
					text += c.Text
				}
			}
		}
		data := map[string]any{"name": u.Update.Title, "source": "inference"}
		if u.Update.Status == "failed" {
			data["error"] = true
		}
		if text != "" {
			data["content"] = text
		}
		emit(Event{Type: "tool.result", Data: data})
	}
}

// ---- acpAdapter：dsh --profile acp 长驻进程 + 会话映射 + 事件转译 ----

type acpAdapter struct {
	bin         []string
	versionArgs []string
	profileArgs []string
}

func newACPAdapter() Backend {
	return &acpAdapter{
		bin:         []string{"dsh"},
		versionArgs: []string{"--version"},
		profileArgs: []string{"--profile", "acp"}, // S0：发行模板自动引导，无需手工建 profile
	}
}

func (a *acpAdapter) Name() string { return "deepseek-harness-acp" }

// Capabilities（S2 翻正面）：Resume=true（session/resume 客户端已实现，真机联调定案会话延续
// 产品语义）；MCP 仍 instruction——MCP http 挂载面虽经 S0 实证可通，平台侧需把 /mcp URL 注入
// dsh profile/session 配置，真机联调后随 Capabilities 一并翻正（诚实标注）。
func (a *acpAdapter) Capabilities() Capabilities {
	return Capabilities{
		Chat: true, Stream: true,
		SkillsMode: "instruction", MCPMode: "instruction",
		AgentAsTool: false, Workflow: false,
		Resume: true,
	}
}

// Probe 与 cliAdapter 同口径：PATH + --version。
func (a *acpAdapter) Probe(ctx context.Context) ProbeResult {
	path, err := exec.LookPath(a.bin[0])
	if err != nil {
		return ProbeResult{Available: false, Reason: fmt.Sprintf("PATH 中未找到 %q（未安装或未加入 PATH）", a.bin[0])}
	}
	vctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(vctx, path, a.versionArgs...).Output()
	if err != nil {
		return ProbeResult{Available: false, Path: path, Reason: fmt.Sprintf("执行 %s --version 失败: %v", a.bin[0], err)}
	}
	version := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	return ProbeResult{Available: true, Version: version, Path: path}
}

// acpUpdate session/update 通知载荷（S0 帧形态：update 字段携带会话更新对象）。
type acpUpdate struct {
	SessionID string `json:"sessionId"`
	Update    struct {
		SessionUpdate string `json:"sessionUpdate"` // agent_message_chunk | tool_call | tool_call_update
		Content       *struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		ToolCallID string          `json:"toolCallId"`
		Title      string          `json:"title"`
		RawInput   json.RawMessage `json:"rawInput"`
		Status     string          `json:"status"`
		Output     *struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	} `json:"update"`
}

// Run 一次推理 = 一个 ACP turn：长驻进程 + session/new（进程内首次）→ session/prompt →
// session/update 事件转译 → 应答携带 stopReason 即 turn 结束。ctx 取消 → session/cancel + 关闭进程。
func (a *acpAdapter) Run(ctx context.Context, req *RunRequest, emit func(Event)) error {
	var permWarn sync.Once
	var sessRef *acpSession
	notifFn := func(method string, params json.RawMessage) {
		if method == "session/request_permission" {
			var pr struct {
				ID *int64 `json:"id"`
			}
			_ = json.Unmarshal(params, &pr)
			if pr.ID != nil && sessRef != nil {
				_ = sessRef.respond(*pr.ID, map[string]any{
					"outcome": map[string]any{"outcome": "cancelled", "optionsId": nil},
				})
			}
			permWarn.Do(func() {
				emit(Event{Type: "run.warning", Data: map[string]any{"message": "ACP 权限请求已按默认 deny 拒绝（会话级审批策略待真机联调升级为转审批）"}})
			})
			return
		}
		if sessRef != nil {
			sessRef.onUpdate(params, emit)
		}
	}
	sess, err := newACPSession(a.bin[0], a.profileArgs, req.Cwd, notifFn)
	if err != nil {
		return err
	}
	sessRef = sess
	defer sess.close()

	// initialize（协议握手；agentCapabilities 校验从宽——S0：版本恒 0.0.1 不校验须自校验）
	if _, err := sess.call(ctx, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{"fs": map[string]any{"readTextFile": false, "writeTextFile": false}},
	}, 15*time.Second); err != nil {
		return fmt.Errorf("ACP initialize 失败: %w", err)
	}
	// session/new：sessionId 平台生成（S0 设计输入：客户端命名可用）；plt- 前缀 + 会话标识。
	sessionID := "plt-run-" + fmt.Sprint(time.Now().UnixNano())
	if req.SessionID != "" {
		sessionID = "plt-" + nonAlnum.ReplaceAllString(req.SessionID, "-")
	}
	if _, err := sess.call(ctx, "session/new", map[string]any{"cwd": orDot(req.Cwd), "sessionId": sessionID, "mcpServers": []any{}}, 15*time.Second); err != nil {
		return fmt.Errorf("ACP session/new 失败: %w", err)
	}
	// session/prompt：应答在 turn 结束时到达（stopReason）。
	result, err := sess.call(ctx, "session/prompt", map[string]any{
		"sessionId": sessionID,
		"content":   []map[string]any{{"type": "text", "text": req.Prompt}},
	}, 30*time.Minute)
	if ctx.Err() != nil {
		_ = sess.notify("session/cancel", map[string]any{"sessionId": sessionID})
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	var out struct {
		StopReason string `json:"stopReason"`
	}
	_ = json.Unmarshal(result, &out)
	if req.Debug >= 2 {
		emit(Event{Type: "debug.cli", Data: map[string]any{"stopReason": out.StopReason, "sessionId": sessionID}})
	}
	return nil
}

// nonAlnum sessionId 消毒（客户端命名自由，但保持帧内可读）。
var nonAlnum = regexp.MustCompile(`[^a-zA-Z0-9_.-]`)

func orDot(s string) string {
	if strings.TrimSpace(s) == "" {
		return "."
	}
	return s
}
