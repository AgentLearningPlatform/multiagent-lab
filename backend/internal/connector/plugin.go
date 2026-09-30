// Package connector REQ-214/M46：外部连接器运行时——平台托管的自研 Go MCP 插件服务。
//
// Kubernetes / SSH 连接器由本插件服务承载（进程内嵌、loopback 独立端口，沿 oo :8092
// 「平台托管」先例；自研服务无需探测式拉起）。装配链（chat.Assembler）对 k8s/ssh 连接器
// 经标准 MCP 客户端管线（tool.FetchMCPTools）访问 /connectors/{id}/mcp，与 mcp 直通连接器
// 同一条管线：{连接器实例名}__{tool} 前缀改名、REQ-14 审批包装与 run_event 审计自动生效。
//
// 安全边界：凭据（kubeconfig 内容 / SSH 私钥口令）服务端绑定——插件服务从 connector 表解密
// 读取，不进 LLM 上下文、不进工具参数；工具入参仅业务语义（resource/pod/command 等）。
package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	mcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// PluginService 连接器插件服务：按连接器实例暴露 MCP 端点 /connectors/{id}/mcp。
// 实例 handler 按 connector.UpdatedAt 缓存（连接器变更后自动重建）。
type PluginService struct {
	Store      *store.Store
	Box        *secrets.Box
	KubectlBin string // 空 = PATH（REQ-191 同口径：运行环境 KUBECTL_BIN 优先级更低的兜底）

	mu      sync.Mutex
	entries map[string]*pluginEntry
}

type pluginEntry struct {
	handler   http.Handler
	builtAtID string // 构建时的 connector.UpdatedAt（变更即重建）
}

func NewPlugin(st *store.Store, box *secrets.Box, kubectlBin string) *PluginService {
	return &PluginService{Store: st, Box: box, KubectlBin: kubectlBin, entries: map[string]*pluginEntry{}}
}

// Handler 返回插件服务根 handler（main.go 挂到 loopback 独立端口）。
func (p *PluginService) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/connectors/"), "/mcp")
		if id == "" || id == r.URL.Path {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		h, err := p.handlerOf(id)
		if err != nil {
			log.Printf("[connector-plugin] connector %s: %v", id, err)
			http.Error(w, `{"error":"connector unavailable"}`, http.StatusBadGateway)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func (p *PluginService) handlerOf(id string) (http.Handler, error) {
	c, err := p.Store.GetConnector(id)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if e, ok := p.entries[id]; ok && e.builtAtID == c.UpdatedAt {
		return e.handler, nil
	}
	srv := mcpserver.NewMCPServer("connector-plugin/"+c.Name, "0.1.0")
	switch c.Kind {
	case store.ConnectorKindKubernetes:
		if err := p.addKubernetesTools(srv, c); err != nil {
			return nil, err
		}
	case store.ConnectorKindSSH:
		if err := p.addSSHTools(srv, c); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("kind %q 不由插件服务承载（mcp 连接器走直通）", c.Kind)
	}
	h := mcpserver.NewStreamableHTTPServer(srv)
	p.entries[id] = &pluginEntry{handler: h, builtAtID: c.UpdatedAt}
	return h, nil
}

// resolveCredentials 解密连接器凭据（服务端绑定；明文仅存在插件服务调用栈内存）。
func (p *PluginService) resolveCredentials(c *store.Connector) (map[string]string, error) {
	out := map[string]string{}
	if len(c.CredentialsEncrypted) == 0 {
		return out, nil
	}
	plain, err := p.Box.Decrypt(c.CredentialsEncrypted)
	if err != nil {
		return nil, fmt.Errorf("decrypt credentials: %w", err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(plain), &m); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out, nil
}

func cfgStr(c *store.Connector, key string) string {
	v, _ := c.Config[key].(string)
	return v
}

func cfgBool(c *store.Connector, key string) bool {
	b, _ := c.Config[key].(bool)
	return b
}

// toolText 统一工具结果文本：stdout + 非零 exit code 透传（验收：exit code/stderr 不吞）。
func toolText(stdout string, err error) string {
	if err == nil {
		if strings.TrimSpace(stdout) == "" {
			return "(命令成功，无输出)"
		}
		return stdout
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(stdout, "\n"))
	if ee, ok := err.(*exitError); ok {
		fmt.Fprintf(&b, "\n[exit code %d] %s", ee.Code, ee.Stderr)
	} else {
		fmt.Fprintf(&b, "\n[error] %s", err.Error())
	}
	return b.String()
}

// KubernetesToolNames Kubernetes 连接器的完整工具名清单（read_only 时剔除 apply）。
func KubernetesToolNames(readOnly bool) []string {
	names := []string{"kubectl_get", "kubectl_describe", "kubectl_logs"}
	if !readOnly {
		names = append(names, "kubectl_apply")
	}
	return names
}

// SSHToolNames SSH 连接器工具名清单。
func SSHToolNames() []string { return []string{"exec"} }

// addKubernetesTools Kubernetes 连接器工具集：kubectl CLI 包装（零新依赖，沿 runtime/k8s.go 口径）。
// config.read_only=true 时跳过 apply（REQ-214 P2⑦：工具级白名单的最轻形态——写操作隐藏）。
func (p *PluginService) addKubernetesTools(srv *mcpserver.MCPServer, c *store.Connector) error {
	creds, err := p.resolveCredentials(c)
	if err != nil {
		return err
	}
	runner := &KubectlRunner{
		Bin:           p.KubectlBin,
		Context:       cfgStr(c, "context"),
		Namespace:     cfgStr(c, "namespace"),
		Kubeconfig:    cfgStr(c, "kubeconfig_path"),
		KubeconfigRAW: creds["kubeconfig"], // 凭据态 kubeconfig 内容（每次调用落临时文件）
	}
	desc := "经 Kubernetes 连接器「" + c.Name + "」访问集群（凭据服务端绑定，无须提供）"

	get := mcp.NewTool("kubectl_get",
		mcp.WithDescription(desc+"：列出/读取资源（kubectl get -o json）"),
		mcp.WithString("resource", mcp.Required(), mcp.Description("资源类型，如 pods / deployments / nodes / services")),
		mcp.WithString("name", mcp.Description("具体资源名（省略=列表）")),
		mcp.WithString("namespace", mcp.Description("命名空间（省略=连接器默认或全命名空间，按资源类型）")),
		mcp.WithString("selector", mcp.Description("标签选择器，如 app=nginx")),
		mcp.WithString("output", mcp.Description("输出格式，默认 json")),
	)
	srv.AddTool(get, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		a := req.GetArguments()
		args := []string{"get", str(a, "resource")}
		if v := str(a, "name"); v != "" {
			args = append(args, v)
		}
		if v := str(a, "namespace"); v != "" {
			args = append(args, "-n", v)
		}
		if v := str(a, "selector"); v != "" {
			args = append(args, "-l", v)
		}
		out := str(a, "output")
		if out == "" {
			out = "json"
		}
		args = append(args, "-o", out)
		stdout, err := runner.Run(ctx, args...)
		return mcp.NewToolResultText(toolText(stdout, err)), nil
	})

	describe := mcp.NewTool("kubectl_describe",
		mcp.WithDescription(desc+"：查看资源详情与事件（kubectl describe）"),
		mcp.WithString("resource", mcp.Required(), mcp.Description("资源类型")),
		mcp.WithString("name", mcp.Required(), mcp.Description("资源名")),
		mcp.WithString("namespace", mcp.Description("命名空间")),
	)
	srv.AddTool(describe, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		a := req.GetArguments()
		args := []string{"describe", str(a, "resource"), str(a, "name")}
		if v := str(a, "namespace"); v != "" {
			args = append(args, "-n", v)
		}
		stdout, err := runner.Run(ctx, args...)
		return mcp.NewToolResultText(toolText(stdout, err)), nil
	})

	logs := mcp.NewTool("kubectl_logs",
		mcp.WithDescription(desc+"：读取 Pod 日志（kubectl logs）"),
		mcp.WithString("pod", mcp.Required(), mcp.Description("Pod 名")),
		mcp.WithString("namespace", mcp.Description("命名空间")),
		mcp.WithString("container", mcp.Description("容器名（多容器 Pod）")),
		mcp.WithNumber("tail", mcp.Description("尾部行数，默认 100")),
		mcp.WithBoolean("previous", mcp.Description("true=上一次崩溃容器的日志")),
	)
	srv.AddTool(logs, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		a := req.GetArguments()
		args := []string{"logs", str(a, "pod")}
		if v := str(a, "namespace"); v != "" {
			args = append(args, "-n", v)
		}
		if v := str(a, "container"); v != "" {
			args = append(args, "-c", v)
		}
		if n, ok := a["tail"].(float64); ok && n > 0 {
			args = append(args, "--tail", fmt.Sprintf("%d", int(n)))
		} else {
			args = append(args, "--tail", "100")
		}
		if b, ok := a["previous"].(bool); ok && b {
			args = append(args, "--previous")
		}
		stdout, err := runner.Run(ctx, args...)
		return mcp.NewToolResultText(toolText(stdout, err)), nil
	})

	if cfgBool(c, "read_only") {
		return nil // 只读模式：写操作工具不注册（REQ-214 P2⑦）
	}
	apply := mcp.NewTool("kubectl_apply",
		mcp.WithDescription(desc+"：应用资源清单（kubectl apply -f -，写操作——建议开启工具审批后使用）"),
		mcp.WithString("manifest", mcp.Required(), mcp.Description("完整的 YAML/JSON 资源清单内容")),
		mcp.WithString("namespace", mcp.Description("命名空间（覆盖清单内 namespace）")),
	)
	srv.AddTool(apply, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		a := req.GetArguments()
		args := []string{"apply", "-f", "-"}
		if v := str(a, "namespace"); v != "" {
			args = append(args, "-n", v)
		}
		stdout, err := runner.RunStdin(ctx, str(a, "manifest"), args...)
		return mcp.NewToolResultText(toolText(stdout, err)), nil
	})
	return nil
}

// addSSHTools SSH 连接器工具集：x/crypto/ssh 无状态 exec-per-call（每次调用一条命令拿全输出返回；
// 交互式 tty/持久会话不做——REQ-214 边界）。
func (p *PluginService) addSSHTools(srv *mcpserver.MCPServer, c *store.Connector) error {
	creds, err := p.resolveCredentials(c)
	if err != nil {
		return err
	}
	runner := &SSHRunner{
		Host: cfgStr(c, "host"), User: cfgStr(c, "user"), Port: cfgStr(c, "port"),
		Password:   creds["password"],
		PrivateKey: creds["private_key"],
		Passphrase: creds["passphrase"],
	}
	exec := mcp.NewTool("exec",
		mcp.WithDescription("经 SSH 连接器「"+c.Name+"」（"+runner.User+"@"+runner.Host+"）执行一条命令并返回全部输出（每次调用独立连接；凭据服务端绑定，无须提供）"),
		mcp.WithString("command", mcp.Required(), mcp.Description("要执行的 shell 命令（非交互式）")),
		mcp.WithNumber("timeout_sec", mcp.Description("超时秒数，默认 30，上限 300")),
	)
	srv.AddTool(exec, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		a := req.GetArguments()
		timeout := 30
		if n, ok := a["timeout_sec"].(float64); ok && n > 0 {
			timeout = int(n)
		}
		stdout, err := runner.Exec(ctx, str(a, "command"), timeout)
		return mcp.NewToolResultText(toolText(stdout, err)), nil
	})
	return nil
}

func str(a map[string]any, key string) string {
	v, _ := a[key].(string)
	return v
}
