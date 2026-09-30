// S1+S2 单测：stub ACP agent（Node 按协议帧形态应答）驱动 streamingProc + acpAdapter。
// 覆盖：initialize/session/new/session/prompt 全链、事件转译（message.chunk/tool_call/tool_call_update）、
// 权限请求默认 deny 应答、ctx 取消关闭阶梯（stdin EOF→SIGTERM→SIGKILL）幂等。
package inference

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const stubACP = `#!/usr/bin/env node
// stub ACP agent：按 31 号 §12 S0 帧形态应答（dsh --profile acp 形态）
let buf = ''
process.stdin.on('data', (d) => {
  buf += d
  let i
  while ((i = buf.indexOf('\n')) >= 0) {
    const line = buf.slice(0, i).trim()
    buf = buf.slice(i + 1)
    if (!line) continue
    let f
    try { f = JSON.parse(line) } catch { continue }
    if (f.id != null && f.method === 'initialize') {
      send({ jsonrpc: '2.0', id: f.id, result: { protocolVersion: 1, agentCapabilities: { sessionCapabilities: { load: false, resume: true, close: true, list: true }, mcpCapabilities: { http: true } } } })
    } else if (f.id != null && f.method === 'session/new') {
      send({ jsonrpc: '2.0', id: f.id, result: { sessionId: f.params.sessionId || 'stub-s1' } })
    } else if (f.id != null && f.method === 'session/prompt') {
      // 真实 ACP：session/prompt 应答在 turn 末才到（stopReason 结算）
      // 权限请求（server→client request）→ 期望客户端默认 deny 应答
      send({ jsonrpc: '2.0', id: 9001, method: 'session/request_permission', params: { sessionId: 's1', toolCall: { title: 'write_file' } } })
      setTimeout(() => {
        send({ jsonrpc: '2.0', method: 'session/update', params: { sessionId: 's1', update: { sessionUpdate: 'agent_message_chunk', content: { type: 'text', text: '你好，' } } } })
        send({ jsonrpc: '2.0', method: 'session/update', params: { sessionId: 's1', update: { sessionUpdate: 'tool_call', toolCallId: 't1', title: 'grep', rawInput: { pattern: 'x' } } } })
        send({ jsonrpc: '2.0', method: 'session/update', params: { sessionId: 's1', update: { sessionUpdate: 'tool_call_update', toolCallId: 't1', title: 'grep', status: 'completed', output: { content: [{ type: 'text', text: 'matched 3 lines' }] } } } })
        send({ jsonrpc: '2.0', method: 'session/update', params: { sessionId: 's1', update: { sessionUpdate: 'agent_message_chunk', content: { type: 'text', text: '完成。' } } } })
        send({ jsonrpc: '2.0', id: f.id, result: { stopReason: 'end_turn' } })
        permDenied = permAsked
      }, 30)
    } else if (f.id === 9001 && f.result) {
      // 客户端对权限请求的应答（默认 deny：cancelled outcome）
      permAsked = true
      permDenied = String(JSON.stringify(f.result)).includes('cancelled')
    }
  }
})
let permAsked = false
let permDenied = false
function send(o) { process.stdout.write(JSON.stringify(o) + '\n') }
process.stdin.on('end', () => process.exit(0))
`

// stubOnPath 把 stub 脚本写入临时目录并返回注入 PATH 的环境前缀。
func stubOnPath(t *testing.T) (binPath string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "dsh")
	if err := os.WriteFile(p, []byte(stubACP), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return p
}

func TestACPAdapterRunEndToEnd(t *testing.T) {
	stubOnPath(t)
	a := newACPAdapter()
	if pr := a.Probe(context.Background()); !pr.Available {
		t.Fatalf("probe: %+v", pr)
	}
	var evs []Event
	req := &RunRequest{Prompt: "测试提示词", SessionID: "conv-abc/1 x"}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.Run(ctx, req, func(e Event) { evs = append(evs, e) }); err != nil {
		t.Fatalf("run: %v", err)
	}
	types := map[string]int{}
	var deltas string
	var toolName, toolResult string
	var permWarned bool
	for _, e := range evs {
		types[e.Type]++
		switch e.Type {
		case "message.delta":
			deltas += e.Data["delta"].(string)
		case "tool.call":
			toolName, _ = e.Data["name"].(string)
		case "tool.result":
			toolResult, _ = e.Data["content"].(string)
		case "run.warning":
			if s, _ := e.Data["message"].(string); len(s) > 0 {
				permWarned = true
			}
		}
	}
	if deltas != "你好，完成。" {
		t.Fatalf("deltas = %q", deltas)
	}
	if toolName != "grep" || toolResult != "matched 3 lines" {
		t.Fatalf("tool events: name=%q result=%q", toolName, toolResult)
	}
	if !permWarned {
		t.Fatalf("权限默认 deny 告警未透出")
	}
	_ = types
}

func TestStreamingProcCloseLadderIdempotent(t *testing.T) {
	stubOnPath(t)
	sess, err := newACPSession("dsh", []string{"--profile", "acp"}, "", func(string, json.RawMessage) {})
	if err != nil {
		t.Fatal(err)
	}
	sess.close()
	sess.close() // 幂等：重复关闭不 panic 不挂死
	select {
	case <-sess.proc.done:
	case <-time.After(8 * time.Second):
		t.Fatalf("关闭阶梯未在 8s 内退出进程")
	}
}

func TestACPAdapterUnavailableWhenNoDSH(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // 空 PATH：dsh 不可达
	a := newACPAdapter()
	pr := a.Probe(context.Background())
	if pr.Available {
		t.Fatalf("dsh 缺席时应不可用")
	}
	if err := a.Run(context.Background(), &RunRequest{Prompt: "x"}, func(Event) {}); err == nil {
		t.Fatalf("dsh 缺席时 Run 应报错（全通道降级可懂）")
	}
}
