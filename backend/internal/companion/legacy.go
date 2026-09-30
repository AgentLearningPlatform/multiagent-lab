package companion

import (
	"context"
	"fmt"
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
// REQ-216 旧伴生实例桥（迁移专用）：:9199 独立 Oxigraph（data/companion-graph）在此
// 最后一次被拉起/领养，用于把存量 agt-{agentID} 图 SPARQL 层复制到方案引擎 ont-{ontologyID}
// 图（MigrateAgentGraphsToOntology），迁移完成即停、此后永不再启——内置实例退役。
// 仅迁移路径引用；confirm/抽取/检索/可视化全链已走 PlanEngines。
// ---------------------------------------------------------------------------

// LegacyInstance 旧伴生实例（迁移期一次性存续）。
type LegacyInstance struct {
	DataDir string // 默认 data/companion-graph
	Port    int    // 固定 9199
	mu      sync.Mutex
	cmd     *exec.Cmd
	base    string
}

// NewLegacyInstance 构造（目录不存在则创建，与旧 Engine 同落点）。
func NewLegacyInstance(dataDir string, port int) *LegacyInstance {
	if dataDir == "" {
		dataDir = filepath.Join("data", "companion-graph")
	}
	if port == 0 {
		port = 9199
	}
	_ = os.MkdirAll(dataDir, 0o755)
	return &LegacyInstance{DataDir: dataDir, Port: port}
}

// candidatePaths 与旧 Engine 探测序一致：显式 env → PATH → data/bin → tools/bin。
func (e *LegacyInstance) candidatePaths() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	add(os.Getenv("COMPANION_OXIGRAPH_BIN"))
	add(os.Getenv("OXIGRAPH_BIN"))
	for _, name := range []string{"oxigraph_server", "oxigraph"} {
		if p, err := exec.LookPath(name); err == nil {
			add(p)
		}
	}
	for _, base := range []string{"data/bin", "../data/bin"} {
		add(filepath.Join(base, "oxigraph_server"))
		add(filepath.Join(base, "oxigraph"))
	}
	add(filepath.Join("tools", "bin", "oxigraph"))
	return out
}

// Reachable 探测旧实例是否已有存活进程（含跨重启遗留）。
func (e *LegacyInstance) Reachable(ctx context.Context) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.base != "" {
		return e.pingLocked(ctx)
	}
	return e.pingAt(ctx, e.endpoint()) == nil
}

func (e *LegacyInstance) endpoint() string {
	return fmt.Sprintf("http://127.0.0.1:%d/query", e.Port)
}

// Ensure 迁移期拉起（已存活直接复用；懒启动一次）。返回 /query 端点。
func (e *LegacyInstance) Ensure(ctx context.Context) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.base != "" && e.pingLocked(ctx) {
		return e.base + "/query", nil
	}
	if e.pingAt(ctx, e.endpoint()) == nil {
		e.base = fmt.Sprintf("http://127.0.0.1:%d", e.Port)
		return e.base + "/query", nil
	}
	var bin string
	for _, p := range e.candidatePaths() {
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			bin = p
			break
		}
	}
	if bin == "" {
		return "", fmt.Errorf("未找到 oxigraph 可执行文件（旧伴生实例迁移不可用）")
	}
	cmd := exec.Command(bin, "serve", "--location", e.DataDir, "--bind", fmt.Sprintf("127.0.0.1:%d", e.Port))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("旧伴生实例启动失败: %w", err)
	}
	e.cmd = cmd
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if e.pingAt(ctx, e.endpoint()) == nil {
			e.base = fmt.Sprintf("http://127.0.0.1:%d", e.Port)
			go func() { _, _ = cmd.Process.Wait() }()
			return e.base + "/query", nil
		}
		select {
		case <-ctx.Done():
			e.stopLocked()
			return "", ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	e.stopLocked()
	return "", fmt.Errorf("旧伴生实例健康等待超时")
}

// Stop 迁移完成后显式停止（此后不再拉起）。
func (e *LegacyInstance) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stopLocked()
}

func (e *LegacyInstance) stopLocked() {
	if e.cmd != nil && e.cmd.Process != nil {
		_ = syscall.Kill(-e.cmd.Process.Pid, syscall.SIGTERM)
	}
	e.cmd = nil
	e.base = ""
}

// Query 迁移期只读查询。
func (e *LegacyInstance) Query(ctx context.Context, endpoint, sparql string) ([]byte, error) {
	tr := &http.Client{Timeout: 30 * time.Second}
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
		return nil, fmt.Errorf("旧实例查询被拒（%s）", resp.Status)
	}
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
		if len(buf) > 16<<20 {
			return nil, fmt.Errorf("旧实例查询结果过大")
		}
	}
	return buf, nil
}

// Update 迁移期变更（DROP 旧图）。
func (e *LegacyInstance) Update(ctx context.Context, sparql string) error {
	tr := &http.Client{Timeout: 30 * time.Second}
	base := strings.TrimSuffix(e.endpoint(), "/query")
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
		return fmt.Errorf("旧实例更新被拒（%s）", resp.Status)
	}
	return nil
}

func (e *LegacyInstance) pingLocked(ctx context.Context) bool {
	return e.pingAt(ctx, e.endpoint()) == nil
}

func (e *LegacyInstance) pingAt(ctx context.Context, endpoint string) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
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
		return fmt.Errorf("旧伴生实例端点异常: %s", resp.Status)
	}
	return nil
}
