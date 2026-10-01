package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

func callTool(t *testing.T, bt einotool.InvokableTool, args string) string {
	t.Helper()
	res, err := bt.InvokableRun(context.Background(), args)
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	return res
}

func tmpRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package main\n// TODO fix me\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# demo\nhello grep 契合\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// grep：正则命中 + 越界安全（根内 walk 不出根）。
func TestGrepTool(t *testing.T) {
	bt, err := NewGrepTool(DirToolDeps{Root: tmpRoot(t)})
	if err != nil {
		t.Fatal(err)
	}
	it := bt.(einotool.InvokableTool)
	res := callTool(t, it, `{"pattern":"TODO"}`)
	if !strings.Contains(res, "src/a.go") || !strings.Contains(res, "TODO fix me") {
		t.Fatalf("grep 未命中: %s", res)
	}
	res2 := callTool(t, it, `{"pattern":"不存在的词"}`)
	if strings.Contains(res2, "a.go") {
		t.Fatalf("不应命中: %s", res2)
	}
}

// glob：非递归与递归（**/）两种模式。
func TestGlobTool(t *testing.T) {
	root := tmpRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "x.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	bt, _ := NewGlobTool(DirToolDeps{Root: root})
	it := bt.(einotool.InvokableTool)
	if res := callTool(t, it, `{"pattern":"**/*.md"}`); !strings.Contains(res, "docs/x.md") {
		t.Fatalf("递归 glob 未命中: %s", res)
	}
	if res := callTool(t, it, `{"pattern":"*.go"}`); strings.Contains(res, "src") {
		t.Fatalf("非递归 glob 不应下钻: %s", res)
	}
}

// write_file：根内写入 + .. 越界拒绝 + append。
func TestWriteFileToolBoundaries(t *testing.T) {
	root := tmpRoot(t)
	bt, _ := NewWriteFileTool(DirToolDeps{Root: root})
	it := bt.(einotool.InvokableTool)
	res := callTool(t, it, `{"path":"out/notes.txt","content":"hi 契合"}`)
	if !strings.Contains(res, "out/notes.txt") {
		t.Fatalf("写入回执异常: %s", res)
	}
	b, _ := os.ReadFile(filepath.Join(root, "out", "notes.txt"))
	if string(b) != "hi 契合" {
		t.Fatalf("内容不符: %q", b)
	}
	if res, err := it.InvokableRun(context.Background(), `{"path":"../escape.txt","content":"x"}`); err != nil || !strings.Contains(res, "不合法") {
		t.Fatalf(".. 越界应以回执拒绝: err=%v res=%s", err, res)
	}
	if _, err := it.InvokableRun(context.Background(), `{"path":"out/notes.txt","content":"\nappended","append":true}`); err != nil {
		t.Fatalf("append 追加应成功: %v", err)
	}
}

// http_fetch：httptest 正常抓取 + 内网黑名单拒绝 + fetchGuard 双保险。
func TestHTTPFetchTool(t *testing.T) {
	bt, _ := NewHTTPFetchTool()
	it := bt.(einotool.InvokableTool)
	// 环回地址：工具内校验拒绝
	if res, err := it.InvokableRun(context.Background(), `{"url":"http://127.0.0.1:1/x"}`); err != nil || !strings.Contains(res, "拒绝") {
		t.Fatalf("环回地址应以回执拒绝: err=%v res=%s", err, res)
	}
	// 公开域名（.invalid 保证解析失败）：应通过守卫、报 DNS 类错误而非守卫拒绝
	if res, err2 := it.InvokableRun(context.Background(), `{"url":"http://example.invalid/x"}`); err2 != nil || strings.Contains(res, "拒绝") || !strings.Contains(res, "抓取失败") {
		t.Fatalf("公开域名应通过守卫报抓取失败回执: err=%v res=%s", err2, res)
	}
	// REQ-224 适配：PreHook 升格为带 guard 名的规格结构体（Fn 为守卫函数）
	guard := NewFetchGuardPre()
	if err := guard.Fn("http_fetch", `{"url":"http://192.168.1.1/x"}`); err == nil {
		t.Fatal("fetchGuard 应拒绝内网")
	}
	if err := guard.Fn("other_tool", `{"url":"http://192.168.1.1/x"}`); err != nil {
		t.Fatalf("非目标工具应放行: %v", err)
	}
	if guard.Guard == "" {
		t.Fatal("PreHook 应带 guard 名（hook.denied 审计载荷需要）")
	}
}

// todo_write：落库往返 + 非法状态拒绝。
func TestTodoWriteTool(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/t.db")
	if err != nil {
		t.Fatal(err)
	}
	ag, _ := st.CreateAgent(&store.Agent{Name: "t"})
	cv, _ := st.CreateConversation(&store.Conversation{Scope: "agent", AgentID: &ag.ID})
	bt, _ := NewTodoWriteTool(TodoDeps{Store: st, ConversationID: cv.ID})
	it := bt.(einotool.InvokableTool)
	res := callTool(t, it, `{"todos":[{"title":"步骤一","status":"done"},{"title":"步骤二","status":"pending"}]}`)
	if !strings.Contains(res, "[x] 步骤一") || !strings.Contains(res, "[ ] 步骤二") {
		t.Fatalf("回执渲染异常: %s", res)
	}
	cv2, _ := st.GetConversation(cv.ID)
	var items []map[string]string
	if err := json.Unmarshal([]byte(cv2.TodoJSON), &items); err != nil || len(items) != 2 {
		t.Fatalf("todo_json 落库异常: %q %v", cv2.TodoJSON, err)
	}
	if res, err := it.InvokableRun(context.Background(), `{"todos":[{"title":"x","status":"doing"}]}`); err != nil || !strings.Contains(res, "status 非法") {
		t.Fatalf("非法状态应以回执拒绝: err=%v res=%s", err, res)
	}
}

// hooks：pre 拒绝阻断执行；链为空不包装。
func TestHookChainWrap(t *testing.T) {
	bt, _ := NewHTTPFetchTool()
	chain := &HookChain{Pre: []PreHook{{Guard: "testGuard", Fn: func(name, args string) error {
		return errDenied
	}}}}
	wrapped := chain.Wrap(bt)
	it := wrapped.(einotool.InvokableTool)
	if res, err := it.InvokableRun(context.Background(), `{"url":"http://example.com"}`); err != nil || !strings.Contains(res, "denied by hook") {
		t.Fatalf("pre hook 拒绝应回执 denied 文本: err=%v res=%s", err, res)
	}
	if (&HookChain{}).Wrap(bt) != bt {
		t.Fatal("空链不应包装")
	}
}

var errDenied = &deniedError{}

type deniedError struct{}

func (*deniedError) Error() string { return "denied by hook" }
