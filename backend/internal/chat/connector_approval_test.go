// REQ-214 验收断言（M46）：tool_approval=all 下连接器工具走审批——装配产物中的
// 连接器工具被审批包装（首次调用 StatefulInterrupt 挂起，ApprovalInfo 带工具名）。
package chat

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/connector"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

func TestConnectorToolUnderApproval(t *testing.T) {
	asm, st, box := newAssemblerFixture(t)
	// 真插件服务（stub kubectl）——审批包装在装配期挂上，工具调用不出网
	plugin := connector.NewPlugin(st, box, stubKubectlForAssembler(t))
	srv := httptest.NewServer(plugin.Handler())
	defer srv.Close()
	asm.PluginEndpoint = srv.URL

	enc, err := box.Encrypt(`{"kubeconfig":"apiVersion: v1"}`)
	if err != nil {
		t.Fatal(err)
	}
	c, err := st.CreateConnector(&store.Connector{
		Kind: store.ConnectorKindKubernetes, Name: "k8s-ops",
		Config:               map[string]any{"namespace": "ops"},
		CredentialsEncrypted: enc,
	})
	if err != nil {
		t.Fatal(err)
	}
	ag := &store.Agent{Name: "guarded-agent", ToolApproval: "all", Connectors: []string{c.ID}}
	tb := asm.assembleOnly(context.Background(), t, ag)

	var kubectlGet einotool.BaseTool
	for _, bt := range tb.Tools {
		if ti, ierr := bt.Info(context.Background()); ierr == nil && ti != nil && ti.Name == "k8s-ops__kubectl_get" {
			kubectlGet = bt
			break
		}
	}
	if kubectlGet == nil {
		t.Fatalf("审批模式应仍装配出连接器工具（告警=%s）", strings.Join(tb.Warnings, "\n"))
	}
	inv, ok := kubectlGet.(einotool.InvokableTool)
	if !ok {
		t.Fatalf("连接器工具应为 InvokableTool，实际 %T", kubectlGet)
	}
	// 首次调用（无恢复上下文）：应挂起审批而非执行——中断信号 Info 含连接器工具名与入参
	args, _ := json.Marshal(map[string]any{"resource": "pods"})
	_, ierr := inv.InvokableRun(context.Background(), string(args))
	if ierr == nil {
		t.Fatal("审批模式下连接器工具首次调用应 StatefulInterrupt 挂起，实际直接执行了")
	}
	if !strings.Contains(ierr.Error(), "k8s-ops__kubectl_get") || !strings.Contains(ierr.Error(), "interrupt") {
		t.Fatalf("中断信息应含连接器工具名: %v", ierr)
	}
}
