// S1/REQ-206（M41）：长驻 stdio 子进程基座——streamingStdioAdapter 的进程持有层。
//
// 与 cliAdapter（一次性执行退出）不同：进程拉起后常驻，多轮会话经同一 stdio 复用；
// stdout 行分帧（JSON-RPC 每行一帧，dsh --profile acp 形态）；关闭阶梯 stdin EOF →
// SIGTERM（3s 宽限）→ SIGKILL，Close 幂等可重入。
package inference

import (
	"bufio"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type streamingProc struct {
	name    string
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	closed  bool
	closeMu sync.Mutex
	sendMu  sync.Mutex
	// done 在进程退出后关闭（waitErr 保存退出状态）
	done    chan struct{}
	waitErr error
}

// startStreaming 拉起长驻子进程并启动 stdout 行泵（onLine 在独立 goroutine 逐行回调）。
func startStreaming(name, path string, args []string, cwd string, env []string, onLine func(line string)) (*streamingProc, error) {
	cmd := exec.Command(path, args...)
	cmd.Env = env
	if cwd != "" {
		cmd.Dir = cwd
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 %s 长驻进程: %w", name, err)
	}
	p := &streamingProc{name: name, cmd: cmd, stdin: stdin, done: make(chan struct{})}
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for sc.Scan() {
			onLine(sc.Text())
		}
		p.waitErr = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

// send 序列化写入一行帧（\n 结尾）并 flush；进程已退出返回错误。
func (p *streamingProc) send(line string) error {
	p.sendMu.Lock()
	defer p.sendMu.Unlock()
	select {
	case <-p.done:
		return fmt.Errorf("%s 进程已退出（%v）", p.name, p.waitErr)
	default:
	}
	if _, err := io.WriteString(p.stdin, line+"\n"); err != nil {
		return fmt.Errorf("写入 %s stdin: %w", p.name, err)
	}
	return nil
}

// close 关闭阶梯（幂等）：stdin EOF → 等 3s → SIGTERM → 等 3s → SIGKILL。
// 不强制等待退出（调用方需要时自行 <-done）。
func (p *streamingProc) close() {
	p.closeMu.Lock()
	defer p.closeMu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	_ = p.stdin.Close() // stdin EOF：协议层优雅退出首选
	select {
	case <-p.done:
		return
	case <-time.After(3 * time.Second):
	}
	_ = p.cmd.Process.Signal(quitSignal())
	select {
	case <-p.done:
		return
	case <-time.After(3 * time.Second):
	}
	_ = p.cmd.Process.Kill()
}
