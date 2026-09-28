package ontochat

import (
	"database/sql"
	"encoding/json"
	"testing"

	_ "modernc.org/sqlite"
)

// M-O14 P2④：对话中间产物持久化与中断恢复——同库重开 Store（模拟进程重启）后
// stage/round/messages/context（含 draft_spec）全量可还原。
func TestSessionPersistenceAcrossReopen(t *testing.T) {
	db, err := sql.Open("sqlite", "file:/tmp/ontochat-persist-test.db?_pragma=busy_timeout(3000)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP TABLE IF EXISTS ontochat_session`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS ontochat_session (
		id TEXT PRIMARY KEY, title TEXT, stage TEXT, round INTEGER,
		messages TEXT, context_json TEXT, ontology_id TEXT DEFAULT '',
		created_at TEXT, updated_at TEXT)`); err != nil {
		t.Fatal(err)
	}

	st := New(db)
	_, err = st.Create("persist-1", "中断恢复冒烟")
	if err != nil {
		t.Fatal(err)
	}
	// CQ 轮：stage/round/context 演进
	stage := "domain"
	round := 2
	ctx := Context{Description: "K8s 运维域", CQs: []string{"哪些组件可部署？"}, Hints: []string{"关注滚动更新"}}
	if err := st.Append("persist-1", Message{Role: "assistant", Content: "归纳要点"}, &stage, &round, &ctx); err != nil {
		t.Fatal(err)
	}
	// 草稿轮：draft_spec 落 context
	raw := json.RawMessage(`{"name":"draft-v1","concepts":[{"name":"Pod"}]}`)
	ctx2 := Context{Description: ctx.Description, CQs: ctx.CQs, Hints: ctx.Hints, DraftSpec: &raw}
	stage2 := "draft"
	if err := st.Append("persist-1", Message{Role: "assistant", Content: "草稿已生成"}, &stage2, &round, &ctx2); err != nil {
		t.Fatal(err)
	}

	// —— 模拟进程重启：新 Store 实例重开同库 ——
	st2 := New(db)
	back, err := st2.Get("persist-1")
	if err != nil {
		t.Fatal(err)
	}
	if back.Stage != "draft" || back.Round != 2 {
		t.Fatalf("stage/round 未还原: %s/%d", back.Stage, back.Round)
	}
	if len(back.Messages) != 2 {
		t.Fatalf("messages 未还原: %d", len(back.Messages))
	}
	if back.Context.Description != "K8s 运维域" || len(back.Context.CQs) != 1 || len(back.Context.Hints) != 1 {
		t.Fatalf("context 未还原: %+v", back.Context)
	}
	if back.Context.DraftSpec == nil {
		t.Fatal("draft_spec 未还原（中断恢复核心断言）")
	}
	var dp map[string]any
	if err := json.Unmarshal(*back.Context.DraftSpec, &dp); err != nil || dp["name"] != "draft-v1" {
		t.Fatalf("draft_spec 内容不符: %v %v", dp, err)
	}
	// 列表口径同样可还原（会话列表恢复）
	list, err := st2.List()
	if err != nil || len(list) == 0 {
		t.Fatalf("List 未还原: %v %d", err, len(list))
	}
}
