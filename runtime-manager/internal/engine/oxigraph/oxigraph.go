// Package oxigraph Oxigraph 官方二进制适配器（P1 唯一引擎，方案 04 §4.2）。
// 启动流程：清空数据目录 → oxigraph_server load 逐本体 bulk load → oxigraph_server serve（SPARQL 端点）。
// 数据目录按 profile 隔离且每次启动重建，保证「显式重载生效」（REQ-87）与幂等。
package oxigraph

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/engine"
)

type Runtime struct {
	Binary  string // oxigraph_server 可执行路径
	DataDir string // 数据根目录（每 profile 一个子目录）
	LogDir  string // 引擎日志目录（每 profile 一个文件）
}

func New(binary, dataDir, logDir string) *Runtime {
	_ = os.MkdirAll(dataDir, 0o755)
	_ = os.MkdirAll(logDir, 0o755)
	return &Runtime{Binary: binary, DataDir: dataDir, LogDir: logDir}
}

// Start 装载并启动。ttls: ontology_id → TTL 内容。
func (r *Runtime) Start(ctx context.Context, profileID string, port int, ttls map[string]string) (*engine.Process, error) {
	dir := filepath.Join(r.DataDir, profileID)
	// 幂等：重建数据目录
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}
	// bulk load 各本体
	for oid, ttl := range ttls {
		f := filepath.Join(dir, "load_"+sanitizeID(oid)+".ttl")
		if err := os.WriteFile(f, []byte(ttl), 0o644); err != nil {
			return nil, err
		}
		cmd := exec.CommandContext(ctx, r.Binary, "load", "--location", dir, "--file", f)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("oxigraph load %s 失败: %v: %s", oid, err, tail(out, 300))
		}
		_ = os.Remove(f)
	}
	// serve
	cmd := exec.Command(r.Binary, "serve", "--location", dir, "--bind", fmt.Sprintf("127.0.0.1:%d", port))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("oxigraph serve 启动失败: %w", err)
	}
	go drainLog(profileID, stderr)

	endpoint := fmt.Sprintf("http://127.0.0.1:%d/query", port)
	p := engine.NewProcess(endpoint, cmd.Process.Pid, func() error {
		// 进程组整体终止（Rust 子进程树）
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _, _ = cmd.Process.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	})
	return p, nil
}

// HealthCheck SPARQL ping：POST 空查询。
func (r *Runtime) HealthCheck(ctx context.Context, endpoint string) error {
	tr := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader("SELECT * WHERE {} LIMIT 1"))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	resp, err := tr.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("SPARQL 端点异常: %s", resp.Status)
	}
	return nil
}

func sanitizeID(id string) string {
	var b strings.Builder
	for _, r := range id {
		if r == '_' || r == '-' || r == '.' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func tail(b []byte, n int) string {
	s := string(b)
	if len(s) > n {
		s = s[len(s)-n:]
	}
	return s
}

// drainLog 引擎 stderr 落盘（logs/{profile}.log，REST /logs 读最近日志）。
func drainLog(path string, r io.Reader) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = io.Copy(f, r)
}
