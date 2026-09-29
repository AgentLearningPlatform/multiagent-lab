// REQ-191/M31 运行环境统一配置——runtime 层单测：
// Scoper 显式作用域覆盖 env（ScopeEffective）、K8sBackend kubeconfig 路径透传（--kubeconfig）。
package runtime

import (
	"context"
	"os"
	"strings"
	"testing"
)

// stubKubectlReadyz 写一个仅应答 readyz 的伪 kubectl（记录全部调用参数）。
func stubKubectlReadyz(t *testing.T, bin, logPath string) {
	t.Helper()
	script := "#!/bin/bash\necho \"$@\" >> \"" + logPath + "\"\n" +
		"case \"$*\" in *\"--raw=/readyz\"*) echo ok ;; *) exit 0 ;; esac\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func callsOfText(t *testing.T, logPath string) []string {
	t.Helper()
	b, err := os.ReadFile(logPath)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

func TestScopeEffectiveOverEnv(t *testing.T) {
	t.Setenv("SANDBOX_SCOPE", "run") // env 配 run，验证 DB 字段优先级
	if got := ScopeEffective(&DockerBackend{}); got != "run" {
		t.Errorf("字段空应回落 env(run)，得到 %s", got)
	}
	if got := ScopeEffective(&DockerBackend{Scope: "agent"}); got != "agent" {
		t.Errorf("字段 agent 应覆盖 env，得到 %s", got)
	}
	if got := ScopeEffective(&K8sBackend{Scope: "garbage"}); got != "run" {
		t.Errorf("字段非法应回落 env(run)，得到 %s", got)
	}
	if got := ScopeEffective(&K8sBackend{Scope: "run"}); got != "run" {
		t.Errorf("k8s 字段 run 应生效，得到 %s", got)
	}
}

func TestK8sKubeconfigPassthrough(t *testing.T) {
	dir := t.TempDir()
	bin := dir + "/kubectl"
	logPath := dir + "/calls.log"
	stubKubectlReadyz(t, bin, logPath)
	k := &K8sBackend{Bin: bin, Kubeconfig: "/etc/platform/kubeconfig", Context: "ctx-a", Namespace: "ns-b"}
	if !k.Available(context.Background()) {
		t.Fatal("stub readyz 应可达")
	}
	calls := callsOfText(t, logPath)
	if len(calls) == 0 {
		t.Fatal("无调用记录")
	}
	first := calls[0]
	for _, want := range []string{"--kubeconfig /etc/platform/kubeconfig", "--context ctx-a", "ns-b", "--raw=/readyz"} {
		if !strings.Contains(first, want) {
			t.Errorf("kubectl 参数缺 %q：got %q", want, first)
		}
	}
	// prepend 顺序使 --context 位于 --kubeconfig 之前（kubectl 全局旗标顺序无关；stub echo
	// 会吞掉字面 -n 选项，namespace 以裸值断言）
	if strings.Index(first, "--kubeconfig") == -1 || strings.Index(first, "--context") > strings.Index(first, "--kubeconfig") {
		t.Errorf("--context 应先于 --kubeconfig（prepend 序）：%q", first)
	}
}
