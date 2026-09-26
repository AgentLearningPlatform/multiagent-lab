package companion

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ---------------------------------------------------------------------------
// REQ-170/M28 伴生图引擎：独立 Oxigraph 实例（低侵入三原则③ 产物独立——
// 数据目录 data/companion-graph/ 独立、整体摘除 = 停进程 + DROP GRAPH + 清候选表；
// 引擎本身 0 改动，二进制复用 REQ-146 的候选序探测）。
// 懒启动：首次确认入图 / 状态查询时拉起，常驻供 P2 检索接入复用。
// ---------------------------------------------------------------------------

// Engine 伴生图 Oxigraph 单例引擎。
type Engine struct {
	Binary   string // 显式指定（COMPANION_OXIGRAPH_BIN）；空 = 自动探测
	DataDir  string // 默认 data/companion-graph
	Port     int    // 默认 9199（运行方案动态端口从 9201 起，不冲突）
	mu       sync.Mutex
	cmd      *exec.Cmd
	endpoint string
}

// NewEngine 构造伴生图引擎（目录不存在则创建）。
func NewEngine(binary, dataDir string, port int) *Engine {
	if dataDir == "" {
		dataDir = filepath.Join("data", "companion-graph")
	}
	if port == 0 {
		port = 9199
	}
	_ = os.MkdirAll(dataDir, 0o755)
	return &Engine{Binary: binary, DataDir: dataDir, Port: port}
}

// candidatePaths 与 runtime-manager oxigraph 适配器同序：显式 → PATH → data/bin → tools/bin。
func (e *Engine) candidatePaths() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	add(e.Binary)
	for _, name := range []string{"oxigraph_server", "oxigraph"} {
		if p, err := exec.LookPath(name); err == nil {
			add(p)
		}
	}
	// backend 进程 cwd 为 backend/（run-dev.sh cd backend），仓库根 data/bin 以 ../data/bin 命中
	for _, base := range []string{"data/bin", "../data/bin"} {
		add(filepath.Join(base, "oxigraph_server"))
		add(filepath.Join(base, "oxigraph"))
	}
	add(filepath.Join("tools", "bin", "oxigraph"))
	return out
}

// resolveBinary 探测可用 oxigraph 可执行文件。
func (e *Engine) resolveBinary() (string, error) {
	for _, p := range e.candidatePaths() {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("未找到 oxigraph 可执行文件（已探测 %s）: 请在本体运行页一键安装或放置 data/bin/oxigraph 后重试", strings.Join(e.candidatePaths(), " → "))
}

// Endpoint 当前端点（未启动返回空）。
func (e *Engine) Endpoint() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.endpoint
}

// ensureStarted 懒启动 oxigraph serve（幂等；已健康直接复用）。
func (e *Engine) ensureStarted(ctx context.Context) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.endpoint != "" {
		if err := e.ping(ctx); err == nil {
			return e.endpoint, nil
		}
		// 端点失活：清状态重拉
		e.endpoint = ""
		e.cmd = nil
	}
	bin, err := e.resolveBinary()
	if err != nil {
		return "", err
	}
	cmd := exec.Command(bin, "serve", "--location", e.DataDir, "--bind", fmt.Sprintf("127.0.0.1:%d", e.Port))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("伴生图 oxigraph 启动失败: %w", err)
	}
	e.cmd = cmd
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/query", e.Port)
	// 健康等待（最多 10s；失败即终止进程报错，不留半启动状态）
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := e.pingLocked(endpoint); err == nil {
			e.endpoint = endpoint
			// 独立回收：主进程退出时随进程组终止（daemon 无需优雅停止；STOP 端点可显式停）
			go func() { _, _ = cmd.Process.Wait() }()
			return endpoint, nil
		}
		select {
		case <-ctx.Done():
			e.stopLocked()
			return "", ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	e.stopLocked()
	return "", fmt.Errorf("伴生图引擎健康等待超时（%s）", endpoint)
}

// Stop 显式停止伴生图引擎（整体摘除路径）。
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopLocked()
}

func (e *Engine) stopLocked() {
	if e.cmd != nil && e.cmd.Process != nil {
		_ = syscall.Kill(-e.cmd.Process.Pid, syscall.SIGTERM)
	}
	e.cmd = nil
	e.endpoint = ""
}

// ping 健康检查（对当前端点）。
func (e *Engine) ping(ctx context.Context) error {
	return e.pingAt(ctx, e.endpoint)
}

func (e *Engine) pingLocked(endpoint string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return e.pingAt(ctx, endpoint)
}

func (e *Engine) pingAt(ctx context.Context, endpoint string) error {
	tr := &http.Client{Timeout: 2 * time.Second}
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
		return fmt.Errorf("伴生图端点异常: %s", resp.Status)
	}
	return nil
}

// Update 发 SPARQL UPDATE（INSERT/DELETE/DROP）到 /update 端点（oxigraph 查询与更新端点分离）。
func (e *Engine) Update(ctx context.Context, sparql string) error {
	if _, err := e.ensureStarted(ctx); err != nil {
		return err
	}
	e.mu.Lock()
	base := strings.TrimSuffix(e.endpoint, "/query")
	e.mu.Unlock()
	tr := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/update", strings.NewReader(sparql))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/sparql-update")
	resp, err := tr.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return fmt.Errorf("伴生图更新被拒（%s）: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// Query 发 SPARQL SELECT，返回原始响应体（JSON）。
func (e *Engine) Query(ctx context.Context, sparql string) ([]byte, error) {
	endpoint, err := e.ensureStarted(ctx)
	if err != nil {
		return nil, err
	}
	tr := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(sparql))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/sparql-query")
	req.Header.Set("Accept", "application/sparql-results+json")
	resp, err := tr.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("伴生图查询被拒（%s）", resp.Status)
	}
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
		if len(buf) > 4<<20 {
			return nil, fmt.Errorf("伴生图查询结果过大")
		}
	}
	return buf, nil
}
