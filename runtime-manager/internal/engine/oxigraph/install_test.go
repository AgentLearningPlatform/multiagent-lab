package oxigraph

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/xiaoyao/eino-multiagent-lab/runtime-manager/internal/engine"
	"testing"
)

// REQ-146 零依赖单测：release 资产平台映射 + 可执行文件候选探测。

func TestReleaseAssetMapping(t *testing.T) {
	cases := []struct {
		goos, goarch, want string
	}{
		{"darwin", "amd64", "oxigraph_v0.5.11_x86_64_apple"},
		{"darwin", "arm64", "oxigraph_v0.5.11_aarch64_apple"},
		{"linux", "amd64", "oxigraph_v0.5.11_x86_64_linux_gnu"},
		{"linux", "arm64", "oxigraph_v0.5.11_aarch64_linux_gnu"},
		{"windows", "amd64", "oxigraph_v0.5.11_x86_64_windows_msvc.exe"},
		{"windows", "arm64", "oxigraph_v0.5.11_aarch64_windows_msvc.exe"},
	}
	for _, c := range cases {
		got, err := ReleaseAsset(c.goos, c.goarch)
		if err != nil || got != c.want {
			t.Fatalf("ReleaseAsset(%s/%s) = %q, %v; want %q", c.goos, c.goarch, got, err, c.want)
		}
	}
	// 无预编译产物平台：返回带指引的错误
	if got, err := ReleaseAsset("plan9", "amd64"); err == nil || got != "" {
		t.Fatalf("unsupported platform 应报错，got %q, %v", got, err)
	}
	// 本机平台必须可映射（一键安装入口可用性）
	if _, err := ReleaseAsset(runtime.GOOS, runtime.GOARCH); err != nil {
		t.Fatalf("本机平台 %s/%s 无映射: %v", runtime.GOOS, runtime.GOARCH, err)
	}
}

func TestFindExecutable(t *testing.T) {
	dir := t.TempDir()
	ok := filepath.Join(dir, "eng_ok")
	if err := os.WriteFile(ok, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	noexec := filepath.Join(dir, "eng_noexec")
	if err := os.WriteFile(noexec, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 可执行文件命中；无执行位/不存在跳过；按候选序取第一个命中
	bin, found := engine.FindExecutable([]string{noexec, filepath.Join(dir, "eng_missing"), ok})
	if !found || bin != ok {
		t.Fatalf("FindExecutable = %q, %v; want %q", bin, found, ok)
	}
	if _, found := engine.FindExecutable([]string{noexec, filepath.Join(dir, "eng_missing")}); found {
		t.Fatal("无执行位/缺失候选不应命中")
	}
	if _, found := engine.FindExecutable(nil); found {
		t.Fatal("空候选不应命中")
	}
}

func TestCandidatePathsDedup(t *testing.T) {
	r := New("oxigraph_server", t.TempDir(), t.TempDir())
	cands := r.candidatePaths()
	seen := map[string]bool{}
	for _, c := range cands {
		if seen[c] {
			t.Fatalf("候选重复: %q (all=%v)", c, cands)
		}
		seen[c] = true
	}
	// 显式配置排首位
	if cands[0] != "oxigraph_server" {
		t.Fatalf("首位应为显式配置的 Binary，got %v", cands)
	}
	// 一键安装落点必须在候选内（装后即时生效的关键）
	joined := ""
	for _, c := range cands {
		joined += c + " "
	}
	for _, want := range []string{"data/bin/oxigraph", "tools/bin/oxigraph"} {
		if !contains(joined, want) {
			t.Fatalf("候选缺少 %q: %v", want, cands)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func TestProbeMissing(t *testing.T) {
	// 无任何可用二进制的环境（Binary 指向不存在的路径；data/bin、tools/bin 候选可能仍命中
	// 开发机存量——此时断言 installed=true 且 binary 非空即可，两种结局都应是自洽状态）
	r := New(filepath.Join(t.TempDir(), "definitely_missing"), t.TempDir(), t.TempDir())
	st := r.Probe()
	if st.Engine != "oxigraph" || !st.Registered || !st.Installable {
		t.Fatalf("status 基本字段: %+v", st)
	}
	if !st.Installed && (st.Hint == "" || len(st.Searched) == 0) {
		t.Fatalf("缺失态应有指引与候选清单: %+v", st)
	}
	if st.Installed && st.Binary == "" {
		t.Fatalf("命中态应有 binary: %+v", st)
	}
}
