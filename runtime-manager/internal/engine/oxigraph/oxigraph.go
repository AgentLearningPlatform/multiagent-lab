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

// PinnedVersion 一键安装 pin 的官方 release 版本（与 run-dev.sh 安装指引一致）。
const PinnedVersion = "v0.5.11"

// ReleaseBaseURL 官方 release 下载基址（一键安装按 GOOS/GOARCH 拼接资产名）。
const ReleaseBaseURL = "https://github.com/oxigraph/oxigraph/releases/download/" + PinnedVersion

// ReleaseAsset 按 GOOS/GOARCH 映射官方 release 资产名（REQ-146；实测 v0.5.11 资产清单，
// darwin/linux/windows 全平台覆盖；无预编译产物的平台返回手动构建指引）。
func ReleaseAsset(goos, goarch string) (string, error) {
	base := "oxigraph_" + PinnedVersion + "_"
	switch {
	case goos == "darwin" && goarch == "amd64":
		return base + "x86_64_apple", nil
	case goos == "darwin" && goarch == "arm64":
		return base + "aarch64_apple", nil
	case goos == "linux" && goarch == "amd64":
		return base + "x86_64_linux_gnu", nil
	case goos == "linux" && goarch == "arm64":
		return base + "aarch64_linux_gnu", nil
	case goos == "windows" && goarch == "amd64":
		return base + "x86_64_windows_msvc.exe", nil
	case goos == "windows" && goarch == "arm64":
		return base + "aarch64_windows_msvc.exe", nil
	}
	return "", fmt.Errorf("当前平台 %s/%s 无官方预编译产物，请从源码构建：cargo install --git https://github.com/oxigraph/oxigraph oxigraph-cli（或参考仓库 README）", goos, goarch)
}

// candidatePaths 可执行文件候选序（REQ-146）：显式配置 → PATH 常见名 → data/bin → tools/bin。
// 与 run-dev.sh 的搜索语义一致；data/bin 为一键安装的落点，装后即命中、无需重启 runtimed。
func (r *Runtime) candidatePaths() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	add(r.Binary)
	if p, err := exec.LookPath("oxigraph_server"); err == nil {
		add(p)
	}
	if p, err := exec.LookPath("oxigraph"); err == nil {
		add(p)
	}
	add("data/bin/oxigraph_server")
	add("data/bin/oxigraph")
	add("tools/bin/oxigraph")
	return out
}

// resolveBinary 探测可用可执行文件；全落空给可自助的安装指引（含一键安装提示）。
func (r *Runtime) resolveBinary() (string, error) {
	cands := r.candidatePaths()
	if bin, ok := engine.FindExecutable(cands); ok {
		return bin, nil
	}
	return "", fmt.Errorf("未找到 oxigraph 可执行文件（已探测 %s）: 可在本体运行页「一键安装」（写入 data/bin，即时生效），或从 https://github.com/oxigraph/oxigraph/releases 下载 %s 并加入 PATH / 放置 data/bin/ 下，或设置 OXIGRAPH_BIN 为完整路径后重启 runtimed",
		strings.Join(cands, " → "), PinnedVersion)
}

// Probe 引擎自检（REQ-146）：installed/binary/version + 缺失时的候选清单与指引。
func (r *Runtime) Probe() engine.EngineStatus {
	st := engine.EngineStatus{Engine: "oxigraph", Registered: true, Installable: true}
	cands := r.candidatePaths()
	st.Searched = cands
	if bin, ok := engine.FindExecutable(cands); ok {
		st.Installed = true
		st.Binary = bin
		st.Version = probeVersion(bin)
		return st
	}
	st.Hint = "未安装：可一键安装（官方 release " + PinnedVersion + "，写入 data/bin，装后即时生效无需重启）；或手动下载后加入 PATH / 放置 data/bin/oxigraph"
	return st
}

// probeVersion best-effort 取 `--version` 首行（失败不阻塞自检）。
func probeVersion(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "--version").CombinedOutput()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	if len(line) > 60 {
		line = line[:60]
	}
	return line
}

// Start 装载并启动。ttls: ontology_id → TTL 内容。
func (r *Runtime) Start(ctx context.Context, profileID string, port int, ttls map[string]string) (*engine.Process, error) {
	// 预检并解析引擎二进制（REQ-146：动态解析，data/bin 一键安装后即时命中）
	bin, rerr := r.resolveBinary()
	if rerr != nil {
		return nil, rerr
	}
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
		cmd := exec.CommandContext(ctx, bin, "load", "--location", dir, "--file", f)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("oxigraph load %s 失败: %v: %s", oid, err, tail(out, 300))
		}
		_ = os.Remove(f)
	}
	// serve
	cmd := exec.Command(bin, "serve", "--location", dir, "--bind", fmt.Sprintf("127.0.0.1:%d", port))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("oxigraph serve 启动失败: %w", err)
	}
	go drainLog(filepath.Join(r.LogDir, profileID+".log"), stderr)

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
