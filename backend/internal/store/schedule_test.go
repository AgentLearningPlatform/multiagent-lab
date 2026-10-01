package store

import (
	"path/filepath"
	"testing"
)

func schedTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return st
}

// schedTestConv 建真实 agent+会话行（conversation_schedule 外键依赖 conversation）。
func schedTestConv(t *testing.T, st *Store, id string) {
	t.Helper()
	ag := &Agent{Name: "t-agent-" + id}
	created, err := st.CreateAgent(ag)
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}
	conv := &Conversation{ID: id, Scope: "agent", Title: "sched-test", AgentID: &created.ID}
	if _, err := st.CreateConversation(conv); err != nil {
		t.Fatalf("create conv: %v", err)
	}
}

// REQ-224/M52：调度持久化 CRUD + run_event schema_version 盖戳。
func TestScheduleCRUD(t *testing.T) {
	st := schedTestStore(t)
	defer st.Close()
	schedTestConv(t, st, "conv-x")

	if sc, _ := st.GetSchedule("conv-x"); sc != nil {
		t.Fatalf("无行应返回 nil")
	}
	if err := st.UpsertSchedule("conv-x", 5, 10); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	sc, err := st.GetSchedule("conv-x")
	if err != nil || sc == nil {
		t.Fatalf("get: %v %+v", err, sc)
	}
	if sc.IntervalMinutes != 5 || sc.MaxRuns != 10 || sc.Done != 0 {
		t.Fatalf("字段不符: %+v", sc)
	}
	// 重设 = 覆盖且 done 归零
	if _, err := st.IncrementScheduleDone("conv-x"); err != nil {
		t.Fatalf("increment: %v", err)
	}
	if err := st.UpsertSchedule("conv-x", 2, 3); err != nil {
		t.Fatalf("upsert2: %v", err)
	}
	sc, _ = st.GetSchedule("conv-x")
	if sc.IntervalMinutes != 2 || sc.Done != 0 {
		t.Fatalf("重设应覆盖并归零: %+v", sc)
	}
	// 计数递增
	d1, _ := st.IncrementScheduleDone("conv-x")
	d2, _ := st.IncrementScheduleDone("conv-x")
	if d1 != 1 || d2 != 2 {
		t.Fatalf("计数 d1=%d d2=%d", d1, d2)
	}
	// 列表
	rows, err := st.ListSchedules()
	if err != nil || len(rows) != 1 {
		t.Fatalf("list: %v %d", err, len(rows))
	}
	if err := st.DeleteSchedule("conv-x"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if sc, _ := st.GetSchedule("conv-x"); sc != nil {
		t.Fatalf("删除后应无行")
	}
}

func TestInsertEventSchemaVersion(t *testing.T) {
	st := schedTestStore(t)
	defer st.Close()
	schedTestConv(t, st, "conv-sv")
	conv := &Conversation{ID: "conv-sv", Scope: "agent", Title: "sv-test"}
	// 显式盖版本
	if _, err := st.InsertEvent(&RunEvent{ConversationID: conv.ID, RunID: "r1", Type: "approval.denied", Data: `{"x":1}`, SchemaVersion: EventSchemaVersion}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// 未盖版本 = 存量兼容按 1 计
	if _, err := st.InsertEvent(&RunEvent{ConversationID: conv.ID, RunID: "r1", Type: "run.started", Data: `{}`}); err != nil {
		t.Fatalf("insert2: %v", err)
	}
	evs, err := st.ListEvents(conv.ID)
	if err != nil || len(evs) != 2 {
		t.Fatalf("list: %v %d", err, len(evs))
	}
	if evs[0].Type == "approval.denied" && evs[0].SchemaVersion != EventSchemaVersion {
		t.Fatalf("新事件应盖版本 %d", EventSchemaVersion)
	}
	if evs[1].Type == "run.started" && evs[1].SchemaVersion != 1 {
		t.Fatalf("未盖版本应回落 1，got %d", evs[1].SchemaVersion)
	}
}
