package connector

// KubectlRunner Kubernetes 连接器的 kubectl CLI 包装（零新依赖，沿 runtime/k8s.go 口径：
// --kubeconfig/--context 透传、命名空间缺省、ExitError stderr 透出）。
// 凭据态 kubeconfig 内容（CredentialsEncrypted 解密）每次调用落 0600 临时文件供 CLI 读取，用后即删。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// exitError 命令非零退出（exit code + stderr 透传给模型，验收口径）。
type exitError struct {
	Code   int
	Stderr string
}

func (e *exitError) Error() string {
	if e.Stderr != "" {
		return fmt.Sprintf("exit code %d: %s", e.Code, e.Stderr)
	}
	return fmt.Sprintf("exit code %d", e.Code)
}

type KubectlRunner struct {
	Bin           string // 空 = kubectl（PATH）
	Context       string // kubectl --context
	Namespace     string // 连接器缺省命名空间（工具入参 -n 优先）
	Kubeconfig    string // kubeconfig 文件路径（config.kubeconfig_path）
	KubeconfigRAW string // 凭据态 kubeconfig 内容（credentials.kubeconfig；非空优先于路径）

	tmpFile string // 每次调用惰性落盘的临时 kubeconfig（进程内缓存，避免逐调用写盘）
}

// kubeconfigPath 解析本次调用使用的 kubeconfig 文件（凭据内容 → 临时文件）。
func (k *KubectlRunner) kubeconfigPath() (string, error) {
	if k.KubeconfigRAW == "" {
		return k.Kubeconfig, nil
	}
	if k.tmpFile == "" {
		dir := filepath.Join(os.TempDir(), "connector-kubeconfig")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", err
		}
		f, err := os.CreateTemp(dir, "kc-*.yaml")
		if err != nil {
			return "", err
		}
		if _, err := f.WriteString(k.KubeconfigRAW); err != nil {
			f.Close()
			return "", err
		}
		if err := f.Chmod(0o600); err != nil {
			f.Close()
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
		k.tmpFile = f.Name()
	}
	return k.tmpFile, nil
}

// args 组装基础参数（--kubeconfig/--context/-n 在子命令前）。
func (k *KubectlRunner) args(args ...string) ([]string, error) {
	bin := k.Bin
	if bin == "" {
		bin = "kubectl"
	}
	out := []string{bin}
	if kc, err := k.kubeconfigPath(); err != nil {
		return nil, err
	} else if kc != "" {
		out = append(out, "--kubeconfig", kc)
	}
	if k.Context != "" {
		out = append(out, "--context", k.Context)
	}
	if k.Namespace != "" {
		out = append(out, "-n", k.Namespace)
	}
	return append(out, args...), nil
}

// Run 执行 kubectl 子命令，返回 stdout；非零退出返回 *exitError。
func (k *KubectlRunner) Run(ctx context.Context, args ...string) (string, error) {
	full, err := k.args(args...)
	if err != nil {
		return "", err
	}
	return runCmd(ctx, nil, full)
}

// RunStdin 带 stdin 执行（kubectl apply -f -）。
func (k *KubectlRunner) RunStdin(ctx context.Context, stdin string, args ...string) (string, error) {
	full, err := k.args(args...)
	if err != nil {
		return "", err
	}
	return runCmd(ctx, strings.NewReader(stdin), full)
}

func runCmd(ctx context.Context, stdin *strings.Reader, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if stdin != nil {
		cmd.Stdin = stdin
	}
	err := cmd.Run()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return out.String(), &exitError{Code: ee.ExitCode(), Stderr: strings.TrimSpace(errBuf.String())}
		}
		return out.String(), err
	}
	return out.String(), nil
}

// Probe 连接测试：kubectl version（校验 CLI + 认证 + 服务端可达）。
func (k *KubectlRunner) Probe(ctx context.Context) (string, error) {
	out, err := k.Run(ctx, "version", "--output=json")
	if err != nil {
		return "", err
	}
	summary := "集群可达"
	// 就近提取服务端版本号（失败不碍事——探测以 exit code 为准）
	if v := extractServerVersion(out); v != "" {
		summary += "（server " + v + "）"
	}
	return summary, nil
}

func extractServerVersion(versionJSON string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(versionJSON), &m); err != nil {
		return ""
	}
	sv, _ := m["serverVersion"].(map[string]any)
	if sv == nil {
		return ""
	}
	git, _ := sv["gitVersion"].(string)
	return git
}
