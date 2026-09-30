// REQ-214/M46 装配链连接器集成单测：Connectors 引用 → 插件服务 MCP 工具装配（stub kubectl）、
// 告警降级路径（插件未启用/连接器缺失/白名单绕过拒绝）、快照脱敏（k8s/ssh 只透出实例名+类型）。
package chat

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/connector"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

func newAssemblerFixture(t *testing.T) (*Assembler, *store.Store, *secrets.Box) {
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
	asm := &Assembler{Store: st, Box: box}
	return asm, st, box
}

// stubKubectl 输出固定 JSON（pods）；返回 bin 路径。
func stubKubectlForAssembler(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "kubectl")
	script := "#!/bin/bash\ncase \"$*\" in *\"get pods\"*) echo '{\"items\":[]}' ;; *) echo '(stub)' ;; esac\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func (a *Assembler) assembleOnly(ctx context.Context, t *testing.T, ag *store.Agent) *toolBundle {
	t.Helper()
	tb, err := a.assembleTools(ctx, ag, assembleScope{})
	if err != nil {
		t.Fatal(err)
	}
	return tb
}

func warningsJoined(tb *toolBundle) string { return strings.Join(tb.Warnings, "\n") }

// k8s 连接器经插件服务走全链：装配出 {实例名}__kubectl_get，来源 mcp:{实例名}。
func TestAssembleConnectorKubernetesEndToEnd(t *testing.T) {
	asm, st, box := newAssemblerFixture(t)
	bin := stubKubectlForAssembler(t)
	p := connector.NewPlugin(st, box, bin)
	srv := httptest.NewServer(p.Handler())
	defer srv.Close()
	asm.PluginEndpoint = srv.URL

	enc, _ := box.Encrypt(`{"kubeconfig":"apiVersion: v1"}`)
	c, err := st.CreateConnector(&store.Connector{
		Kind: store.ConnectorKindKubernetes, Name: "k8s-ops",
		Config: map[string]any{"namespace": "ops"}, CredentialsEncrypted: enc,
	})
	if err != nil {
		t.Fatal(err)
	}
	ag := &store.Agent{Name: "ops-agent", Connectors: []string{c.ID}}
	tb := asm.assembleOnly(context.Background(), t, ag)

	found := ""
	for _, bt := range tb.Tools {
		if ti, ierr := bt.Info(context.Background()); ierr == nil && ti != nil && ti.Name == "k8s-ops__kubectl_get" {
			found = ti.Name
		}
	}
	if found == "" {
		t.Fatalf("应装配出 k8s-ops__kubectl_get，告警=%s", warningsJoined(tb))
	}
	if tb.SourceOf[found] != "mcp:k8s-ops" {
		t.Fatalf("来源标注: %s", tb.SourceOf[found])
	}
	// 快照脱敏：mcp 字段仅实例名+类型，不含 config/凭据
	snap := asm.mcpSnapshotOf(ag)
	if len(snap) != 1 || snap[0].Name != "k8s-ops" || snap[0].URL != "kubernetes 连接器" {
		t.Fatalf("快照脱敏: %+v", snap)
	}
}

// 插件服务未启用：k8s/ssh 连接器降级告警，不阻断装配。
func TestAssembleConnectorPluginDisabled(t *testing.T) {
	asm, st, box := newAssemblerFixture(t)
	enc, _ := box.Encrypt(`{"password":"x"}`)
	c, _ := st.CreateConnector(&store.Connector{
		Kind: store.ConnectorKindSSH, Name: "ops-ssh",
		Config: map[string]any{"host": "10.0.0.5"}, CredentialsEncrypted: enc,
	})
	ag := &store.Agent{Name: "a", Connectors: []string{c.ID}}
	tb := asm.assembleOnly(context.Background(), t, ag)
	if !strings.Contains(warningsJoined(tb), "插件服务") {
		t.Fatalf("应告警插件服务未启用: %s", warningsJoined(tb))
	}
}

// 连接器不存在：告警跳过。
func TestAssembleConnectorMissing(t *testing.T) {
	asm, st, _ := newAssemblerFixture(t)
	_ = st
	ag := &store.Agent{Name: "a", Connectors: []string{"no-such"}}
	tb := asm.assembleOnly(context.Background(), t, ag)
	if !strings.Contains(warningsJoined(tb), "不存在") {
		t.Fatalf("应告警连接器不存在: %s", warningsJoined(tb))
	}
}

// 授权防线：mcp 直通 URL 指向插件服务基址 = 绕过白名单，拒绝装配。
func TestAssembleConnectorPluginLoopGuard(t *testing.T) {
	asm, st, _ := newAssemblerFixture(t)
	base := "http://127.0.0.1:8093"
	asm.PluginEndpoint = base
	c, _ := st.CreateConnector(&store.Connector{
		Kind: store.ConnectorKindMCP, Name: "evil",
		Config: map[string]any{"url": base + "/connectors/other/mcp"},
	})
	ag := &store.Agent{Name: "a", Connectors: []string{c.ID}}
	tb := asm.assembleOnly(context.Background(), t, ag)
	if !strings.Contains(warningsJoined(tb), "绕过连接器授权") {
		t.Fatalf("应拒绝白名单绕过: %s", warningsJoined(tb))
	}
}
