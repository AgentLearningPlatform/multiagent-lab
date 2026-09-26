package rest

// REQ-155 工具链统一调用面集成测试：临时 SQLite + httptest，零外部依赖。
// 覆盖 validate（按 ontology_id）/ version（清单+快照）/ diff（草稿对照模式）/ 未知工具 404。

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/ontology-service/internal/repo"
)

const toolchainSpecJSON = `{"name":"医学","concepts":[
	{"name":"疾病","definition":"机体异常生命活动"},
	{"name":"症状","parents":["疾病"]}],
 "relations":[{"name":"表现为","from":"疾病","to":"症状"}],
 "instances":[{"name":"感冒","concept":"疾病"}]}`

const toolchainDraftJSON = `{"name":"医学","concepts":[
	{"name":"疾病","definition":"机体在病因作用下异常生命活动"},
	{"name":"症状","parents":["疾病"]},
	{"name":"检查"}],
 "relations":[{"name":"表现为","from":"疾病","to":"症状"}],
 "instances":[{"name":"感冒","concept":"疾病"}]}`

func newToolchainTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	st, err := repo.Open(filepath.Join(t.TempDir(), "toolchain_test.db"), "../../migrations")
	if err != nil {
		t.Fatalf("打开临时库失败: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.CreateOntology("tont", "工具链测试本体", ""); err != nil {
		t.Fatalf("建本体失败: %v", err)
	}
	if err := st.PutArtifact("tont", "spec_json", toolchainSpecJSON, true); err != nil {
		t.Fatalf("写 spec artifact 失败: %v", err)
	}
	if err := st.SaveVersion("tont", 1, toolchainSpecJSON, "", ""); err != nil {
		t.Fatalf("写版本 1 失败: %v", err)
	}
	mux := http.NewServeMux()
	New(st, nil, nil).Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func postTool(t *testing.T, srv *httptest.Server, tool, body string) (int, []byte) {
	t.Helper()
	resp, err := http.Post(srv.URL+"/api/ontology/toolchain/"+tool, "application/json", bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return resp.StatusCode, buf
}

func TestToolchainValidateByOntologyID(t *testing.T) {
	srv := newToolchainTestServer(t)
	code, body := postTool(t, srv, "validate", `{"ontology_id":"tont","strict":true}`)
	if code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", code, body)
	}
	for _, want := range []string{`"tool":"validate"`, `"pass":true`, `"quality":`} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("响应缺少 %s: %s", want, body)
		}
	}
}

func TestToolchainLintInlineSpec(t *testing.T) {
	srv := newToolchainTestServer(t)
	code, body := postTool(t, srv, "lint", `{"spec":{"name":"内联","concepts":[{"name":"A"}]}}`)
	if code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", code, body)
	}
	if !bytes.Contains(body, []byte(`"tool":"lint"`)) || !bytes.Contains(body, []byte(`"findings":[`)) {
		t.Fatalf("lint 响应形状不符: %s", body)
	}
}

func TestToolchainVersionListAndSnapshot(t *testing.T) {
	srv := newToolchainTestServer(t)
	code, body := postTool(t, srv, "version", `{"ontology_id":"tont","version":1}`)
	if code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", code, body)
	}
	for _, want := range []string{`"versions":[`, `"spec":`, `"name":"医学"`} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("响应缺少 %s: %s", want, body)
		}
	}
}

func TestToolchainDiffDraftAgainstCurrent(t *testing.T) {
	srv := newToolchainTestServer(t)
	code, body := postTool(t, srv, "diff", `{"ontology_id":"tont","spec":`+toolchainDraftJSON+`}`)
	if code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", code, body)
	}
	for _, want := range []string{`"from_version":"current"`, `"to_version":"draft"`, `"added":`, `"impact":`} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("响应缺少 %s: %s", want, body)
		}
	}
}

func TestToolchainUnknownTool(t *testing.T) {
	srv := newToolchainTestServer(t)
	code, body := postTool(t, srv, "nope", `{}`)
	if code != http.StatusNotFound {
		t.Fatalf("未知工具期望 404，实际 %d: %s", code, body)
	}
}
