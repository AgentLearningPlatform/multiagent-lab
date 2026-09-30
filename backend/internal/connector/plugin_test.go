// REQ-214/M46 连接器插件服务端到端单测（stub kubectl）：
// 经标准 MCP 客户端管线（tool.FetchMCPTools）访问 /connectors/{id}/mcp——工具前缀改名/调用/
// 凭据服务端注入（临时 kubeconfig）/非零 exit code 透传；凭据不进工具参数与快照。
package connector

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/tool"
)

// stubKubectl 记录参数并按资源分流：pods → JSON 输出；secret-resource → exit 7（非零透传用例）。
func stubKubectl(t *testing.T) (bin, logPath string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "kubectl")
	logPath = filepath.Join(dir, "calls.log")
	script := "#!/bin/bash\necho \"$@\" >> \"" + logPath + "\"\n" +
		"case \"$*\" in\n" +
		"  *\"get pods\"*) echo '{\"items\":[{\"metadata\":{\"name\":\"pod-a\"}}]}' ;;\n" +
		"  *\"get secret-resource\"*) echo 'rbac denied' >&2; exit 7 ;;\n" +
		"  *\" version \"*) echo '{\"serverVersion\":{\"gitVersion\":\"v1.31.0\"}}' ;;\n" +
		"  *) echo '(stub ok)' ;;\n" +
		"esac\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, logPath
}

func newPluginFixture(t *testing.T) (*PluginService, *store.Store, *secrets.Box) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	box, err := secrets.LoadKeyFile(filepath.Join(t.TempDir(), ".secret"))
	if err != nil {
		t.Fatal(err)
	}
	return NewPlugin(st, box, ""), st, box
}

// 凭据态 kubeconfig（加密落库 → 插件服务解密 → 临时文件注入 CLI；内容含哨兵串）。
const fakeKubeconfig = "apiVersion: v1\nkind: Config\n# SENTINEL-KUBECONFIG"

func encryptKubeconfig(t *testing.T, box *secrets.Box) []byte {
	t.Helper()
	b, err := box.Encrypt(`{"kubeconfig":` + strconv.Quote(fakeKubeconfig) + `}`)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPluginKubernetesMCPEndToEnd(t *testing.T) {
	p, st, box := newPluginFixture(t)
	bin, logPath := stubKubectl(t)
	p.KubectlBin = bin
	enc := encryptKubeconfig(t, box)
	c, err := st.CreateConnector(&store.Connector{
		Kind: store.ConnectorKindKubernetes, Name: "k8s-ops",
		Config:               map[string]any{"context": "ctx-a", "namespace": "ns-a"},
		CredentialsEncrypted: enc,
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()

	// 1) 标准 MCP 客户端管线：列工具 + {实例名}__{tool} 前缀
	bts, err := tool.FetchMCPTools(context.Background(), c.Name, srv.URL+"/connectors/"+c.ID+"/mcp", 10e9)
	if err != nil {
		t.Fatalf("fetch tools: %v", err)
	}
	want := []string{"k8s-ops__kubectl_get", "k8s-ops__kubectl_describe", "k8s-ops__kubectl_logs", "k8s-ops__kubectl_apply"}
	got := map[string]einotool.BaseTool{}
	for _, bt := range bts {
		ti, _ := bt.Info(context.Background())
		got[ti.Name] = bt
	}
	for _, w := range want {
		if _, ok := got[w]; !ok {
			t.Fatalf("缺工具 %s，实际 %v", w, keysOf(got))
		}
	}

	// 2) 调用 kubectl_get：参数/凭据服务端注入（--kubeconfig 临时文件含哨兵内容）
	inv, ok := got["k8s-ops__kubectl_get"].(einotool.InvokableTool)
	if !ok {
		t.Fatal("kubectl_get 应为 InvokableTool")
	}
	out, err := inv.InvokableRun(context.Background(), marshalArgs(map[string]any{"resource": "pods"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "pod-a") {
		t.Fatalf("输出应含 stub 数据: %s", out)
	}
	calls, _ := os.ReadFile(logPath)
	// -n ns-a（连接器缺省命名空间）与 --context ctx-a 透传
	if !strings.Contains(string(calls), "-n ns-a") || !strings.Contains(string(calls), "--context ctx-a") {
		t.Fatalf("调用参数缺失: %s", calls)
	}
	// --kubeconfig 指向的临时文件内容 = 凭据 kubeconfig
	kc := kubeconfigArgOf(string(calls))
	if kc == "" {
		t.Fatal("调用应带 --kubeconfig")
	}
	content, err := os.ReadFile(kc)
	if err != nil || !strings.Contains(string(content), "SENTINEL-KUBECONFIG") {
		t.Fatalf("临时 kubeconfig 应含凭据内容: %v %s", err, content)
	}

	// 3) 非零 exit code 透传
	out, err = inv.InvokableRun(context.Background(), marshalArgs(map[string]any{"resource": "secret-resource"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[exit code 7]") || !strings.Contains(out, "rbac denied") {
		t.Fatalf("exit code/stderr 应透传: %s", out)
	}
	_ = bin
}

func marshalArgs(m map[string]any) string {
	b, _ := json.Marshal(m)
	return string(b)
}

// 工具入参仅业务语义：凭证/凭据字段不在 inputSchema。
func TestPluginToolSchemaNoCredentials(t *testing.T) {
	p, st, box := newPluginFixture(t)
	enc := encryptKubeconfig(t, box)
	c, _ := st.CreateConnector(&store.Connector{
		Kind: store.ConnectorKindKubernetes, Name: "k8s-x",
		Config:               map[string]any{"kubeconfig_path": "/tmp/kc.yaml"},
		CredentialsEncrypted: enc,
	})
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()
	bts, err := tool.FetchMCPTools(context.Background(), c.Name, srv.URL+"/connectors/"+c.ID+"/mcp", 10e9)
	if err != nil {
		t.Fatal(err)
	}
	for _, bt := range bts {
		ti, _ := bt.Info(context.Background())
		if strings.Contains(strings.ToLower(ti.Desc), "kubeconfig") && strings.Contains(ti.Desc, "SENTINEL") {
			t.Fatalf("工具描述泄漏凭据: %s", ti.Desc)
		}
		// inputSchema 序列化不含凭据字段（kubectl_get 属性白名单）
		if strings.HasSuffix(ti.Name, "kubectl_get") {
			raw, _ := json.Marshal(ti)
			for _, forbidden := range []string{"kubeconfig", "password", "private_key"} {
				if strings.Contains(string(raw), `"`+forbidden+`"`) {
					t.Fatalf("inputSchema 不应含 %s: %s", forbidden, raw)
				}
			}
		}
	}
}

// mcp kind 不由插件服务承载（走直通），未知 kind 502。
func TestPluginRejectsMCPKind(t *testing.T) {
	p, st, _ := newPluginFixture(t)
	c, _ := st.CreateConnector(&store.Connector{Kind: store.ConnectorKindMCP, Name: "plain", Config: map[string]any{"url": "http://x/mcp"}})
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()
	if _, err := tool.FetchMCPTools(context.Background(), c.Name, srv.URL+"/connectors/"+c.ID+"/mcp", 5e9); err == nil {
		t.Fatal("mcp kind 应被插件服务拒绝")
	}
}

func keysOf(m map[string]einotool.BaseTool) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

func kubeconfigArgOf(calls string) string {
	for _, line := range strings.Split(calls, "\n") {
		f := strings.Fields(line)
		for i, a := range f {
			if a == "--kubeconfig" && i+1 < len(f) {
				return f[i+1]
			}
		}
	}
	return ""
}

// exitError 文案（toolText 合流行为）。
func TestToolTextExitCode(t *testing.T) {
	if s := toolText("out", &exitError{Code: 7, Stderr: "boom"}); !strings.Contains(s, "[exit code 7] boom") {
		t.Fatalf("toolText: %s", s)
	}
	if s := toolText("", nil); s != "(命令成功，无输出)" {
		t.Fatalf("空输出提示: %s", s)
	}
}
