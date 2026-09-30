// REQ-214/M46：外部连接器 REST API（设置页「连接器」分区 + AgentSidePanel 授权勾选数据源）。
// 凭据入参仅落库瞬间存在（AES-256-GCM 加密），响应永不回传明文（has_credentials 布尔替代）；
// 连接测试沿 REQ-191 先例（探测可用性并回写 status，不做重试风暴）。
package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/connector"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/tool"
)

// connectorOut 列表/详情出参（凭据脱敏）。
type connectorOut struct {
	ID             string         `json:"id"`
	Kind           string         `json:"kind"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	Config         map[string]any `json:"config"`
	HasCredentials bool           `json:"has_credentials"`
	Status         string         `json:"status"`
	StatusDetail   string         `json:"status_detail"`
	IsBuiltin      bool           `json:"is_builtin"`
	Refs           []string       `json:"refs"` // 引用该连接器的 agent 名（删除保护）
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
}

func (s *Server) connectorOut(c *store.Connector) connectorOut {
	refs, err := s.Store.ConnectorRefs(c.ID)
	if err != nil {
		refs = []string{}
	}
	if refs == nil {
		refs = []string{}
	}
	return connectorOut{
		ID: c.ID, Kind: c.Kind, Name: c.Name, Description: c.Description,
		Config: c.Config, HasCredentials: c.HasCredentials,
		Status: c.Status, StatusDetail: c.StatusDetail, IsBuiltin: c.IsBuiltin,
		Refs: refs, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt,
	}
}

// listConnectors GET /api/connectors。
func (s *Server) listConnectors(w http.ResponseWriter, r *http.Request) {
	cs, err := s.Store.ListConnectors()
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]connectorOut, 0, len(cs))
	for _, c := range cs {
		out = append(out, s.connectorOut(c))
	}
	writeJSON(w, http.StatusOK, map[string]any{"connectors": out})
}

type connectorIn struct {
	Kind        string         `json:"kind"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Config      map[string]any `json:"config"`
	Credentials map[string]any `json:"credentials"` // 明文仅此一瞬，加密落库后丢弃
}

// validateConnectorIn kind/name/config 结构校验（kind 创建后不可改——config/credentials 结构随之绑定）。
func validateConnectorIn(in *connectorIn) *store.HTTPError {
	kindOK := false
	for _, k := range store.ConnectorKinds {
		if in.Kind == k {
			kindOK = true
			break
		}
	}
	if !kindOK {
		return &store.HTTPError{Status: http.StatusBadRequest, Msg: "kind 取值须为 mcp|kubernetes|ssh"}
	}
	if in.Name == "" {
		return &store.HTTPError{Status: http.StatusBadRequest, Msg: "name 必填（连接器实例名，装配前缀槽位）"}
	}
	switch in.Kind {
	case store.ConnectorKindMCP:
		if urlOf(in.Config) == "" {
			return &store.HTTPError{Status: http.StatusBadRequest, Msg: "自定义 MCP 连接器须提供 config.url（Streamable HTTP MCP 端点）"}
		}
	case store.ConnectorKindKubernetes:
		if strOf(in.Config, "kubeconfig_path") == "" && credStr(in.Credentials, "kubeconfig") == "" {
			return &store.HTTPError{Status: http.StatusBadRequest, Msg: "Kubernetes 连接器须提供 config.kubeconfig_path 或 credentials.kubeconfig（二选一）"}
		}
	case store.ConnectorKindSSH:
		if strOf(in.Config, "host") == "" {
			return &store.HTTPError{Status: http.StatusBadRequest, Msg: "SSH 连接器须提供 config.host"}
		}
	}
	return nil
}

func urlOf(cfg map[string]any) string { return strOf(cfg, "url") }

func strOf(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	v, _ := cfg[key].(string)
	return v
}

func credStr(creds map[string]any, key string) string { return strOf(creds, key) }

// encryptCredentials 凭据 map → AES-256-GCM 密文（空 map 返回 nil=不写凭据）。
func (s *Server) encryptCredentials(creds map[string]any) ([]byte, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(creds)
	if err != nil {
		return nil, err
	}
	return s.Box.Encrypt(string(b))
}

// createConnector POST /api/connectors。
func (s *Server) createConnector(w http.ResponseWriter, r *http.Request) {
	var in connectorIn
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if he := validateConnectorIn(&in); he != nil {
		writeErr(w, he)
		return
	}
	enc, err := s.encryptCredentials(in.Credentials)
	if err != nil {
		writeErr(w, err)
		return
	}
	c := &store.Connector{
		Kind: in.Kind, Name: in.Name, Description: in.Description,
		Config: in.Config, CredentialsEncrypted: enc,
	}
	if in.Credentials != nil && len(in.Credentials) > 0 {
		c.Status = "unknown" // 新凭据未验证
	}
	created, err := s.Store.CreateConnector(c)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.connectorOut(created))
}

// errConnectorBuiltin 内置实例不可删除/改核心字段。
var errConnectorBuiltin = &store.HTTPError{Status: http.StatusBadRequest, Msg: "内置连接器不可删除（可编辑描述）"}

// updateConnector PUT /api/connectors/{id}。
// 凭据口径沿模型 API Key 先例：credentials 缺省/空 = 保留原值；内置行 name/kind/config/凭据锁死（仅描述可改）。
func (s *Server) updateConnector(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.GetConnector(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	var in connectorIn
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	if c.IsBuiltin {
		c.Description = in.Description
		updated, err := s.Store.UpdateConnector(c)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, s.connectorOut(updated))
		return
	}
	kind := c.Kind
	name := c.Name
	if in.Name != "" {
		name = in.Name
	}
	config := c.Config
	if in.Config != nil {
		config = in.Config
	}
	// 凭据：未携带（nil 或空对象）= 保留原值；携带 = 重新加密覆盖
	if len(in.Credentials) > 0 {
		enc, err := s.encryptCredentials(in.Credentials)
		if err != nil {
			writeErr(w, err)
			return
		}
		c.CredentialsEncrypted = enc
		c.Status = "unknown"
	}
	c.Kind = kind
	c.Name = name
	c.Description = in.Description
	c.Config = config
	updated, err := s.Store.UpdateConnector(c)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.connectorOut(updated))
}

// deleteConnector DELETE /api/connectors/{id}：内置拦截；被 agent 引用时 409 附引用者。
func (s *Server) deleteConnector(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.GetConnector(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	if c.IsBuiltin {
		writeErr(w, errConnectorBuiltin)
		return
	}
	refs, err := s.Store.ConnectorRefs(c.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(refs) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "连接器仍被智能体引用，请先在智能体侧板取消授权",
			"refs":  refs,
		})
		return
	}
	if err := s.Store.DeleteConnector(c.ID); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// testConnector POST /api/connectors/{id}/test：按 kind 探测连接并回写 status。
// mcp = 真 MCP 握手（initialize + list_tools，报告工具数）；kubernetes = kubectl 集群探针；
// ssh = 拨号 + 认证。探测仅验证可达性，不做重试风暴（REQ-191 口径）。
func (s *Server) testConnector(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.GetConnector(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	ok := false
	detail := ""
	switch c.Kind {
	case store.ConnectorKindMCP:
		var bts, ferr = func() (int, error) {
			tools, e := tool.FetchMCPTools(ctx, c.Name, urlOf(c.Config), 12*time.Second)
			return len(tools), e
		}()
		ok = ferr == nil
		if ok {
			detail = "可达，暴露 " + strconv.Itoa(bts) + " 个工具"
		} else {
			detail = "不可达：" + ferr.Error()
		}
	case store.ConnectorKindKubernetes:
		ok, detail = s.testKubernetesConnector(ctx, c)
	case store.ConnectorKindSSH:
		ok, detail = s.testSSHConnector(ctx, c)
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "未知连接器类型 " + c.Kind})
		return
	}
	c.Status = map[bool]string{true: "ok", false: "error"}[ok]
	c.StatusDetail = detail
	if _, err := s.Store.UpdateConnector(c); err != nil {
		log.Printf("[connectors] test 回写状态失败: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "detail": detail, "status": c.Status})
}

// resolveCredentials 解密凭据 JSON（无凭据返回空 map）。明文仅在调用栈内存存在。
func (s *Server) resolveCredentials(c *store.Connector) (map[string]any, error) {
	out := map[string]any{}
	if len(c.CredentialsEncrypted) == 0 {
		return out, nil
	}
	plain, err := s.Box.Decrypt(c.CredentialsEncrypted)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(plain), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// testKubernetesConnector kubectl 探针（CLI + 认证 + 服务端可达）。
func (s *Server) testKubernetesConnector(ctx context.Context, c *store.Connector) (bool, string) {
	creds, err := s.resolveCredentials(c)
	if err != nil {
		return false, "凭据解密失败：" + err.Error()
	}
	kcRaw, _ := creds["kubeconfig"].(string)
	runner := &connector.KubectlRunner{
		Bin:           s.kubectlBin(),
		Context:       strOf(c.Config, "context"),
		Namespace:     strOf(c.Config, "namespace"),
		Kubeconfig:    strOf(c.Config, "kubeconfig_path"),
		KubeconfigRAW: kcRaw,
	}
	detail, err := runner.Probe(ctx)
	if err != nil {
		return false, "不可达：" + err.Error()
	}
	return true, detail
}

// testSSHConnector 拨号 + 认证 + echo 探针。
func (s *Server) testSSHConnector(ctx context.Context, c *store.Connector) (bool, string) {
	creds, err := s.resolveCredentials(c)
	if err != nil {
		return false, "凭据解密失败：" + err.Error()
	}
	pw, _ := creds["password"].(string)
	pk, _ := creds["private_key"].(string)
	pp, _ := creds["passphrase"].(string)
	port := strOf(c.Config, "port")
	runner := &connector.SSHRunner{
		Host: strOf(c.Config, "host"), User: strOf(c.Config, "user"), Port: port,
		Password: pw, PrivateKey: pk, Passphrase: pp,
	}
	detail, err := runner.Probe(ctx)
	if err != nil {
		return false, "不可达：" + err.Error()
	}
	return true, detail
}

// kubectlBin kubectl 路径：运行环境配置优先，env 兜底（REQ-191 同口径）。
func (s *Server) kubectlBin() string {
	if s.RuntimeEnv != nil && s.RuntimeEnv.Defaults.KubectlBin != "" {
		return s.RuntimeEnv.Defaults.KubectlBin
	}
	return os.Getenv("KUBECTL_BIN")
}
