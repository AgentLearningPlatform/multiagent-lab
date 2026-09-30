// REQ-214/M46 连接器 API 单测：CRUD/凭据脱敏（明文与密文均不回传）/删除保护（引用 409、内置 400）/test 状态回写。
package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

func newConnectorAPIFixture(t *testing.T) *Server {
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
	return &Server{Store: st, Box: box}
}

func doConnectorReq(s *Server, method, target string, body any) *httptest.ResponseRecorder {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, rd)
	// 提取路径参数（/api/connectors/{id}[/test]）
	if rest, ok := strings.CutPrefix(target, "/api/connectors/"); ok {
		id := rest
		if i := strings.Index(id, "/"); i >= 0 {
			id = id[:i]
		}
		req.SetPathValue("id", id)
	}
	switch method {
	case "GET":
		s.listConnectors(w, req)
	case "POST":
		if strings.HasSuffix(target, "/test") {
			s.testConnector(w, req)
		} else {
			s.createConnector(w, req)
		}
	case "PUT":
		s.updateConnector(w, req)
	case "DELETE":
		s.deleteConnector(w, req)
	}
	return w
}

func TestConnectorAPIEncryptedAndMasked(t *testing.T) {
	s := newConnectorAPIFixture(t)
	// 创建（SSH 连接器带凭据）
	w := doConnectorReq(s, "POST", "/api/connectors", map[string]any{
		"kind": "ssh", "name": "ops-ssh",
		"config":  map[string]any{"host": "10.0.0.5", "user": "ops"},
		"credentials": map[string]any{"password": "SUPER-SECRET"},
	})
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		ID             string `json:"id"`
		HasCredentials bool   `json:"has_credentials"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if !out.HasCredentials {
		t.Fatal("has_credentials 应为 true")
	}
	// 脱敏断言：响应体不含明文，也不含密文（BASE64 形态）
	if strings.Contains(w.Body.String(), "SUPER-SECRET") {
		t.Fatal("响应泄漏明文凭据")
	}
	// 库内为密文
	c, _ := s.Store.GetConnectorByName("ops-ssh")
	if len(c.CredentialsEncrypted) == 0 || bytes.Contains(c.CredentialsEncrypted, []byte("SUPER-SECRET")) {
		t.Fatal("库内应为密文而非明文")
	}
	// 列表脱敏（明文与凭据字段均不回传；has_credentials 布尔除外）
	w = doConnectorReq(s, "GET", "/api/connectors", nil)
	if strings.Contains(w.Body.String(), "SUPER-SECRET") || strings.Contains(w.Body.String(), `"credentials"`) {
		t.Fatal("列表泄漏凭据（明文或字段）")
	}
	// 更新不带凭据 = 保留
	w = doConnectorReq(s, "PUT", "/api/connectors/"+out.ID, map[string]any{
		"kind": "ssh", "name": "ops-ssh", "description": "改描述",
		"config": map[string]any{"host": "10.0.0.6", "user": "ops"},
	})
	if w.Code != 200 {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	c2, _ := s.Store.GetConnector(out.ID)
	if len(c2.CredentialsEncrypted) == 0 {
		t.Fatal("更新未带凭据应保留原凭据")
	}
	// 更新携带新凭据 = 覆盖
	w = doConnectorReq(s, "PUT", "/api/connectors/"+out.ID, map[string]any{
		"kind": "ssh", "name": "ops-ssh",
		"config":      map[string]any{"host": "10.0.0.6", "user": "ops"},
		"credentials": map[string]any{"password": "NEW-SECRET"},
	})
	c3, _ := s.Store.GetConnector(out.ID)
	if bytes.Contains(c3.CredentialsEncrypted, []byte("CIPHER")) || len(c3.CredentialsEncrypted) == 0 {
		t.Fatal("新凭据应重新加密")
	}
	plain, err := s.Box.Decrypt(c3.CredentialsEncrypted)
	if err != nil || !strings.Contains(plain, "NEW-SECRET") {
		t.Fatalf("新凭据应可解密: %v %s", err, plain)
	}
}

func TestConnectorDeleteProtection(t *testing.T) {
	s := newConnectorAPIFixture(t)
	if err := s.Store.EnsureBuiltinOpenOntologiesConnector(); err != nil {
		t.Fatal(err)
	}
	builtin, _ := s.Store.GetConnectorByName(store.BuiltinConnectorOpenOntologies)
	// 内置不可删
	w := doConnectorReq(s, "DELETE", "/api/connectors/"+builtin.ID, nil)
	if w.Code != 400 {
		t.Fatalf("内置删除应 400, got %d", w.Code)
	}
	// 被 agent 引用 → 409
	c, _ := s.Store.CreateConnector(&store.Connector{Kind: store.ConnectorKindMCP, Name: "refd", Config: map[string]any{"url": "http://x/mcp"}})
	if _, err := s.Store.CreateAgent(&store.Agent{Name: "user-agent", MaxIteration: 5, Connectors: []string{c.ID}}); err != nil {
		t.Fatal(err)
	}
	w = doConnectorReq(s, "DELETE", "/api/connectors/"+c.ID, nil)
	if w.Code != 409 {
		t.Fatalf("引用删除应 409, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "user-agent") {
		t.Fatal("409 应附引用者列表")
	}
	// 无引用可删
	if _, err := s.Store.CreateAgent(&store.Agent{Name: "lonely", MaxIteration: 5}); err != nil {
		t.Fatal(err)
	}
	c2, _ := s.Store.CreateConnector(&store.Connector{Kind: store.ConnectorKindMCP, Name: "free", Config: map[string]any{"url": "http://x/mcp"}})
	w = doConnectorReq(s, "DELETE", "/api/connectors/"+c2.ID, nil)
	if w.Code != 200 {
		t.Fatalf("无引用删除应 200, got %d %s", w.Code, w.Body.String())
	}
}

// kind 校验白名单 + mcp 必填 config.url。
func TestConnectorValidation(t *testing.T) {
	s := newConnectorAPIFixture(t)
	w := doConnectorReq(s, "POST", "/api/connectors", map[string]any{"kind": "grpc", "name": "x"})
	if w.Code != 400 {
		t.Fatal("未知 kind 应 400")
	}
	w = doConnectorReq(s, "POST", "/api/connectors", map[string]any{"kind": "mcp", "name": "x"})
	if w.Code != 400 {
		t.Fatal("mcp 缺 url 应 400")
	}
	w = doConnectorReq(s, "POST", "/api/connectors", map[string]any{"kind": "ssh", "name": "x"})
	if w.Code != 400 {
		t.Fatal("ssh 缺 host 应 400")
	}
}

// test 端点：mcp 不可达 → status=error 回写；列表透出。
func TestConnectorTestWritesStatus(t *testing.T) {
	s := newConnectorAPIFixture(t)
	c, _ := s.Store.CreateConnector(&store.Connector{Kind: store.ConnectorKindMCP, Name: "dead",
		Config: map[string]any{"url": "http://127.0.0.1:1/mcp"}})
	w := doConnectorReq(s, "POST", "/api/connectors/"+c.ID+"/test", nil)
	if w.Code != 200 {
		t.Fatalf("test: %d", w.Code)
	}
	var out struct {
		OK     bool   `json:"ok"`
		Status string `json:"status"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if out.OK || out.Status != "error" {
		t.Fatalf("不可达应 error: %+v", out)
	}
	got, _ := s.Store.GetConnector(c.ID)
	if got.Status != "error" {
		t.Fatalf("status 应回写 error, got %s", got.Status)
	}
}
