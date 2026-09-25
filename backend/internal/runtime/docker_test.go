// 10a 补强单测：docker CLI 路径解析（DOCKER_BIN > PATH > 常见安装位置）。零外部依赖。
package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDockerBinResolution(t *testing.T) {
	d := &DockerBackend{Bin: "/custom/docker"}
	if got := d.dockerBin(); got != "/custom/docker" {
		t.Fatalf("显式 Bin 应优先: %s", got)
	}

	// DOCKER_BIN 环境变量次优
	t.Setenv("DOCKER_BIN", "/env/docker")
	d2 := &DockerBackend{}
	if got := d2.dockerBin(); got != "/env/docker" {
		t.Fatalf("DOCKER_BIN 应次优: %s", got)
	}

	// PATH 可命中时返回 "docker"（LookPath 语义）
	t.Setenv("DOCKER_BIN", "")
	t.Setenv("PATH", "/usr/bin:/bin") // 无 docker 的 PATH
	d3 := &DockerBackend{}
	got := d3.dockerBin()
	if got != "docker" && !fileExecutable(got) {
		t.Fatalf("PATH 无 docker 时应回退常见安装位置或保留原始名: %s", got)
	}

	// fileExecutable 语义：可执行文件命中；目录/缺失/无执行位不命中
	dir := t.TempDir()
	ok := filepath.Join(dir, "ok")
	if err := os.WriteFile(ok, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !fileExecutable(ok) {
		t.Fatal("可执行文件应命中")
	}
	noexec := filepath.Join(dir, "noexec")
	_ = os.WriteFile(noexec, []byte("x"), 0o644)
	if fileExecutable(noexec) || fileExecutable(dir) || fileExecutable(filepath.Join(dir, "missing")) {
		t.Fatal("目录/缺失/无执行位不应命中")
	}
}
