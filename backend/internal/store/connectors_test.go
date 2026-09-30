// REQ-214/M46 外部连接器存储层单测：CRUD/名称冲突/凭据保留口径/存量 mcp_servers 幂等迁移/oo 内置引导。零依赖。
package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func newConnectorTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestConnectorCRUDAndNameConflict(t *testing.T) {
	st := newConnectorTestStore(t)
	c, err := st.CreateConnector(&Connector{Kind: ConnectorKindMCP, Name: "my-mcp", Config: map[string]any{"url": "http://127.0.0.1:9001/mcp"}})
	if err != nil {
		t.Fatal(err)
	}
	if c.ID == "" || c.Status != "unknown" {
		t.Fatalf("create: %+v", c)
	}
	if _, err := st.CreateConnector(&Connector{Kind: ConnectorKindMCP, Name: "my-mcp", Config: map[string]any{"url": "http://x"}}); err != ErrConflict {
		t.Fatalf("同名应 ErrConflict, got %v", err)
	}
	c.Description = "更新后"
	updated, err := st.UpdateConnector(c)
	if err != nil || updated.Description != "更新后" {
		t.Fatalf("update: %v %+v", err, updated)
	}
	if err := st.DeleteConnector(c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetConnector(c.ID); err != ErrNotFound {
		t.Fatalf("删除后应 ErrNotFound, got %v", err)
	}
}

// 凭据更新口径沿模型 API Key 先例：nil = 保留原值。
func TestConnectorCredentialsPreservedOnUpdate(t *testing.T) {
	st := newConnectorTestStore(t)
	c, err := st.CreateConnector(&Connector{Kind: ConnectorKindSSH, Name: "ops-ssh",
		Config: map[string]any{"host": "10.0.0.5"},
		CredentialsEncrypted: []byte("CIPHERTEXT")})
	if err != nil {
		t.Fatal(err)
	}
	c.Description = "只改描述"
	updated, err := st.UpdateConnector(c) // CredentialsEncrypted 已携带（原值透传）
	if err != nil || string(updated.CredentialsEncrypted) != "CIPHERTEXT" {
		t.Fatalf("原值透传应保留: %v %+v", err, updated)
	}
	// 置 nil（UpdateConnector 内回读保留）
	updated2, err := st.UpdateConnector(&Connector{ID: c.ID, Kind: c.Kind, Name: c.Name,
		Description: "再改", Config: c.Config, CredentialsEncrypted: nil})
	if err != nil || string(updated2.CredentialsEncrypted) != "CIPHERTEXT" {
		t.Fatalf("nil 应保留原值: %v %+v", err, updated2)
	}
}

// 存量迁移：mcp_servers → connector 实例（同名复用）+ agent.connectors 引用 + mcp_servers 置空；二次运行幂等。
func TestMigrateAgentMCPToConnectorsIdempotent(t *testing.T) {
	st := newConnectorTestStore(t)
	ag, err := st.CreateAgent(&Agent{Name: "ops-agent", Instruction: "x", MaxIteration: 5,
		MCPServers: []MCPServer{
			{Name: "open-ontologies", URL: "http://127.0.0.1:8092/mcp"},
			{Name: "legacy-mcp", URL: "http://127.0.0.1:9001/mcp"},
		}})
	if err != nil {
		t.Fatal(err)
	}
	// 预置同名连接器（迁移应复用而非重建）
	if _, err := st.CreateConnector(&Connector{Kind: ConnectorKindMCP, Name: "open-ontologies",
		Config: map[string]any{"url": "http://127.0.0.1:8092/mcp"}}); err != nil {
		t.Fatal(err)
	}
	n, err := st.MigrateAgentMCPToConnectors()
	if err != nil || n != 1 {
		t.Fatalf("migrate n=1, got n=%d err=%v", n, err)
	}
	got, _ := st.GetAgent(ag.ID)
	if len(got.MCPServers) != 0 {
		t.Fatalf("mcp_servers 应置空, got %+v", got.MCPServers)
	}
	if len(got.Connectors) != 2 {
		t.Fatalf("connectors 应 2 项, got %+v", got.Connectors)
	}
	// 同名复用：总数 2（预置 1 + 新建 1）
	cs, _ := st.ListConnectors()
	if len(cs) != 2 {
		t.Fatalf("连接器总数应 2, got %d", len(cs))
	}
	// 幂等：二次迁移 0
	if n2, err := st.MigrateAgentMCPToConnectors(); err != nil || n2 != 0 {
		t.Fatalf("二次迁移应 n=0, got n=%d err=%v", n2, err)
	}
}

func TestEnsureBuiltinOpenOntologiesConnector(t *testing.T) {
	st := newConnectorTestStore(t)
	if err := st.EnsureBuiltinOpenOntologiesConnector(); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureBuiltinOpenOntologiesConnector(); err != nil { // 幂等
		t.Fatal(err)
	}
	c, err := st.GetConnectorByName(BuiltinConnectorOpenOntologies)
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsBuiltin || c.Kind != ConnectorKindMCP {
		t.Fatalf("内置 oo 连接器属性: %+v", c)
	}
	if !strings.Contains(c.Description, "/api/oo/mcp") {
		t.Fatalf("描述应含同源等价地址: %s", c.Description)
	}
}

// Agent.connectors 列读写回路。
func TestAgentConnectorsRoundTrip(t *testing.T) {
	st := newConnectorTestStore(t)
	c, _ := st.CreateConnector(&Connector{Kind: ConnectorKindMCP, Name: "c1", Config: map[string]any{"url": "http://x/mcp"}})
	ag, err := st.CreateAgent(&Agent{Name: "a1", MaxIteration: 5, Connectors: []string{c.ID}})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetAgent(ag.ID)
	if len(got.Connectors) != 1 || got.Connectors[0] != c.ID {
		t.Fatalf("connectors 回路: %+v", got.Connectors)
	}
	got.Connectors = []string{}
	if _, err := st.UpdateAgent(got); err != nil {
		t.Fatal(err)
	}
	got2, _ := st.GetAgent(ag.ID)
	if len(got2.Connectors) != 0 {
		t.Fatalf("清空回路: %+v", got2.Connectors)
	}
}
