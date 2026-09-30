package rest

// REQ-145/M22 二批 A3 交付测试：按版本 spec 快照只读端点
// （GET /api/ontologies/{id}/versions/{version}/spec）——前端文本 diff 的数据面。
// 临时 SQLite + httptest，零外部依赖。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/repo"
)

func newSpecEndpointTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := repo.Open(filepath.Join(t.TempDir(), "rest_test.db"), "../../migrations")
	if err != nil {
		t.Fatalf("打开临时库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.CreateOntology("ontest", "测试本体", ""); err != nil {
		t.Fatalf("建本体失败: %v", err)
	}
	if err := st.SaveVersion("ontest", 1, `{"name":"v1"}`, "", ""); err != nil {
		t.Fatalf("写版本 1 失败: %v", err)
	}
	if err := st.SaveVersion("ontest", 2, `{"name":"v2"}`, "turtle", "<a> <b> <c>."); err != nil {
		t.Fatalf("写版本 2 失败: %v", err)
	}
	mux := http.NewServeMux()
	New(st, nil, nil).Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestVersionSpecReturnsSnapshotRaw(t *testing.T) {
	srv := newSpecEndpointTestServer(t)
	resp, err := http.Get(srv.URL + "/api/ontologies/ontest/versions/2/spec")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type 期望 application/json; charset=utf-8，实际 %q", ct)
	}
	buf := make([]byte, 256)
	n, _ := resp.Body.Read(buf)
	if string(buf[:n]) != `{"name":"v2"}` {
		t.Fatalf("快照原文不匹配: %q", string(buf[:n]))
	}
}

func TestVersionSpecUnknownVersionNotFound(t *testing.T) {
	srv := newSpecEndpointTestServer(t)
	resp, err := http.Get(srv.URL + "/api/ontologies/ontest/versions/9/spec")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("未知版本期望 404，实际 %d", resp.StatusCode)
	}
}

func TestVersionSpecRejectsInvalidVersion(t *testing.T) {
	srv := newSpecEndpointTestServer(t)
	for _, v := range []string{"0", "-1", "abc"} {
		resp, err := http.Get(srv.URL + "/api/ontologies/ontest/versions/" + v + "/spec")
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("版本 %q 期望 400，实际 %d", v, resp.StatusCode)
		}
	}
}

// REQ-215 guide 紧凑化：指引只含本体身份+规模统计+工具用法，
// 不再枚举概念/关系全量清单（概念发现移交 facade list_concepts 运行时按需拉取）。
func TestGuideCompactNoEnumeration(t *testing.T) {
	st, err := repo.Open(filepath.Join(t.TempDir(), "guide_test.db"), "../../migrations")
	if err != nil {
		t.Fatalf("打开临时库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.CreateOntology("ontest", "测试本体", "测试描述"); err != nil {
		t.Fatalf("建本体失败: %v", err)
	}
	spec := `{"name":"测试本体","description":"测试描述",
		"concepts":[{"name":"部署"},{"name":"服务"}],
		"relations":[{"name":"包含","from":"部署","to":"服务"}],
		"instances":[{"name":"i1","concept":"部署"},{"name":"i2","concept":"部署"},{"name":"i3","concept":"服务"}]}`
	if err := st.PutArtifact("ontest", "spec_json", spec, true); err != nil {
		t.Fatalf("写 spec 失败: %v", err)
	}
	mux := http.NewServeMux()
	New(st, nil, nil).Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/ontologies/ontest/guide")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("期望 200，实际 %d", resp.StatusCode)
	}
	var out struct {
		OntologyID string `json:"ontology_id"`
		Guide      string `json:"guide"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if out.OntologyID != "ontest" {
		t.Fatalf("ontology_id 不符: %q", out.OntologyID)
	}
	for _, want := range []string{
		"可用本体【测试本体】",
		"规模：概念 2 个、关系 1 个、实例 3 个",
		"list_concepts",
		"ontology_id 固定为 ontest",
	} {
		if !strings.Contains(out.Guide, want) {
			t.Fatalf("guide 缺少 %q：\n%s", want, out.Guide)
		}
	}
	for _, banned := range []string{"概念：", "包含(部署→服务)", "、i1"} {
		if strings.Contains(out.Guide, banned) {
			t.Fatalf("guide 不应再含枚举段 %q：\n%s", banned, out.Guide)
		}
	}
}
