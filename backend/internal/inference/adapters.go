package inference

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// einoADK 平台自研默认后端（§6.16.3）：实际推理由 chat 内进程管线执行，
// 注册进 Registry 仅为探测清单完整性（Status.Available 恒真、完整能力矩阵）。
type einoADK struct{}

func newEinoADK() *einoADK { return &einoADK{} }

func (e *einoADK) Name() string { return DefaultBackend }

func (e *einoADK) Probe(_ context.Context) ProbeResult {
	return ProbeResult{Available: true, Reason: "平台内进程自研管线，无需外部探测"}
}

func (e *einoADK) Capabilities() Capabilities {
	return Capabilities{
		Chat: true, Stream: true,
		SkillsMode: "tools", MCPMode: "tools",
		AgentAsTool: true, Workflow: true, Resume: true,
	}
}

func (e *einoADK) Run(_ context.Context, _ *RunRequest, _ func(Event)) error {
	return fmt.Errorf("eino-adk 由平台内进程管线执行，不经后端适配器调用")
}

// cliAdapter 外部 CLI 适配器公共骨架：PATH 探测 + 子进程执行 + stdout 行解析。
type cliAdapter struct {
	name        string
	bin         []string            // 候选可执行名（依次 LookPath）
	versionArgs []string            // 探测版本参数
	buildArgs   func(req *RunRequest) []string
	parseLine   func(line string, emit func(Event)) // 逐行解析 stdout（nil = 整段输出一条 delta）
	linePrefix  string              // 非 nil 时每条 delta 追加的行尾符
}

func (c *cliAdapter) Name() string { return c.name }

func (c *cliAdapter) Capabilities() Capabilities {
	// §6.16.4 外部 CLI 能力降级：对话/流式✅（行级转译），技能/MCP→prompt 注入⚠️，
	// AgentAsTool/工作流❌（P1 不暴露），中断恢复仅 Cancel。
	return Capabilities{
		Chat: true, Stream: true,
		SkillsMode: "instruction", MCPMode: "instruction",
		AgentAsTool: false, Workflow: false, Resume: false,
	}
}

// Probe PATH 探测 + --version（5s 超时），版本取首个非空 stdout 行。
func (c *cliAdapter) Probe(ctx context.Context) ProbeResult {
	path, err := exec.LookPath(c.bin[0])
	if err != nil {
		// 依次尝试候选名（如 opencode 某些安装形态为 opencode-linux 等）
		for _, cand := range c.bin[1:] {
			if p, e := exec.LookPath(cand); e == nil {
				path, err = p, nil
				break
			}
		}
	}
	if err != nil {
		return ProbeResult{Available: false, Reason: fmt.Sprintf("PATH 中未找到 %q（未安装或未加入 PATH）", c.bin[0])}
	}
	vctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(vctx, path, c.versionArgs...).Output()
	if err != nil {
		return ProbeResult{Available: false, Path: path, Reason: fmt.Sprintf("执行 %s %s 失败: %v", filepath.Base(path), strings.Join(c.versionArgs, " "), err)}
	}
	version := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	return ProbeResult{Available: true, Version: version, Path: path}
}

// Run 启动子进程，stdout 逐行交给 parseLine（或整段输出），stderr 缓存用于错误信息。
func (c *cliAdapter) Run(ctx context.Context, req *RunRequest, emit func(Event)) error {
	path, err := exec.LookPath(c.bin[0])
	if err != nil {
		return fmt.Errorf("推理后端 %q 不可用: PATH 中未找到 %q", c.name, c.bin[0])
	}
	args := c.buildArgs(req)
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = os.Environ()
	for k, v := range req.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if req.Cwd != "" {
		cmd.Dir = req.Cwd
	}
	cmd.WaitDelay = 3 * time.Second // ctx 取消后等待子进程退出的宽限期

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 %s: %w", c.name, err)
	}

	if c.parseLine == nil {
		// 无逐行解析：整段收集，结束后一次 delta
		buf, _ := io.ReadAll(stdout)
		_ = cmd.Wait()
		if text := strings.TrimSpace(string(buf)); text != "" {
			emit(Event{Type: "message.delta", Data: map[string]any{"delta": text}})
		}
	} else {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			c.parseLine(sc.Text(), emit)
		}
		_ = cmd.Wait()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() != 0 {
		tail := stderr.String()
		if len(tail) > 800 {
			tail = tail[len(tail)-800:]
		}
		return fmt.Errorf("%s 退出码 %d: %s", c.name, cmd.ProcessState.ExitCode(), strings.TrimSpace(tail))
	}
	return nil
}

// ---- claude-code：--print stream-json（JSONL 事件流） ----

type ccLine struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Result  string `json:"result"`
	Message *struct {
		Content []struct {
			Type string          `json:"type"`
			Text string          `json:"text"`
			Name string          `json:"name"`
			Raw  json.RawMessage `json:"input"`
		} `json:"content"`
	} `json:"message"`
}

// parseClaudeCodeLine 解析 claude -p --output-format stream-json 的 JSONL：
// assistant 消息中的 text → message.delta，tool_use → tool.call；result 行兜底输出最终文本。
func parseClaudeCodeLine(line string, emit func(Event)) {
	line = strings.TrimSpace(line)
	if line == "" || line[0] != '{' {
		return
	}
	var l ccLine
	if json.Unmarshal([]byte(line), &l) != nil {
		return
	}
	switch l.Type {
	case "assistant":
		if l.Message == nil {
			return
		}
		for _, blk := range l.Message.Content {
			switch blk.Type {
			case "text":
				if blk.Text != "" {
					emit(Event{Type: "message.delta", Data: map[string]any{"delta": blk.Text}})
				}
			case "tool_use":
				data := map[string]any{"name": blk.Name, "source": "inference"}
				if len(blk.Raw) > 0 {
					var input map[string]any
					if json.Unmarshal(blk.Raw, &input) == nil {
						data["args"] = input
					}
				}
				emit(Event{Type: "tool.call", Data: data})
			}
		}
	case "result":
		if l.Result != "" {
			emit(Event{Type: "message.delta", Data: map[string]any{"delta": "\n" + l.Result}})
		}
	}
}

// ---- opencode / aider：纯文本输出，行级转译 ----

// parsePlainLine 纯文本行 → message.delta；过滤 CLI UI 噪声行。
func parsePlainLine(noise func(string) bool) func(string, func(Event)) {
	return func(line string, emit func(Event)) {
		if noise != nil && noise(line) {
			return
		}
		emit(Event{Type: "message.delta", Data: map[string]any{"delta": line + "\n"}})
	}
}

func isNoiseLine(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false // 保留空行（段落分隔）
	}
	for _, p := range []string{"> ", "─", "━", "⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	for _, p := range []string{"Use /help", "Aider v", "Warning: ", "Model: ", "Git repo: ", "Added ", "https://aider"} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// newCLIAdapters 全部内置外部 CLI 适配器（§6.16.3 ②）。
func newCLIAdapters() []Backend {
	return []Backend{
		&cliAdapter{
			// claude -p <prompt> --output-format stream-json --verbose
			// 注记：方案原文为 --input-format stream-json（stdin 喂消息）；实现取提示词走 argv 传参，
			// 免去 stdin 协议对接，事件语义等价（JSONL → message.delta/tool.call）。
			name:        "claude-code",
			bin:         []string{"claude"},
			versionArgs: []string{"--version"},
			buildArgs: func(req *RunRequest) []string {
				return []string{"-p", req.Prompt, "--output-format", "stream-json", "--verbose"}
			},
			parseLine: parseClaudeCodeLine,
		},
		&cliAdapter{
			// opencode run <prompt>：非交互一次性执行（方案原文为 serve HTTP + SSE；
			// 实现取 run 子命令，同等降级语义且无会话生命周期管理，注记见 docs 02）
			name:        "opencode",
			bin:         []string{"opencode"},
			versionArgs: []string{"--version"},
			buildArgs: func(req *RunRequest) []string {
				return []string{"run", req.Prompt}
			},
			parseLine: parsePlainLine(nil),
		},
		&cliAdapter{
			// aider --message <prompt> --no-auto-commits --no-git --yes-always
			name:        "aider",
			bin:         []string{"aider"},
			versionArgs: []string{"--version"},
			buildArgs: func(req *RunRequest) []string {
				args := []string{"--message", req.Prompt, "--no-auto-commits", "--no-git", "--yes-always", "--no-check-update"}
				if req.Cwd != "" {
					args = append(args, "--files", ".")
				}
				return args
			},
			parseLine: parsePlainLine(isNoiseLine),
		},
	}
}
