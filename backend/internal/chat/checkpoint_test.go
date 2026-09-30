package chat

import (
	"context"
	"testing"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// REQ-204 C1：持久化 checkpoint——Set/Get/Delete 往返 + 覆盖写 + 不存在 ok=false。
func TestStoreCheckPointStore(t *testing.T) {
	st, err := store.Open(t.TempDir() + "/cp.db")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := NewStoreCheckPointStore(st)
	ctx := context.Background()
	if _, ok, _ := s.Get(ctx, "cp1"); ok {
		t.Fatal("不存在应 ok=false")
	}
	if err := s.Set(ctx, "cp1", []byte("gob-blob-契合")); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(ctx, "cp1", []byte("v2")); err != nil { // 覆盖写
		t.Fatal(err)
	}
	b, ok, err := s.Get(ctx, "cp1")
	if err != nil || !ok || string(b) != "v2" {
		t.Fatalf("往返失败: ok=%v b=%q err=%v", ok, b, err)
	}
	if err := s.Delete(ctx, "cp1"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get(ctx, "cp1"); ok {
		t.Fatal("删除后应不存在")
	}
}
