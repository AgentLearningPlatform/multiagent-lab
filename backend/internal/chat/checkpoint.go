// 中断检查点存储（M11 收尾 · 中断恢复）：eino ADK 在中断/取消时把运行状态 gob 存入
// CheckPointStore，恢复时按 checkpointID 取回重放。学习尺度单机进程内实现即可；
// 进程重启后 checkpoint 丢失（挂起中断失效，会话重新提问时自动清理挂起状态——诚实边界）。
package chat

import (
	"context"
	"sync"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// CheckPoints 会话中断检查点存储抽象（ADK core.CheckPointStore + 可选删除）。
type CheckPoints interface {
	Get(ctx context.Context, checkPointID string) ([]byte, bool, error)
	Set(ctx context.Context, checkPointID string, checkPoint []byte) error
	Delete(ctx context.Context, checkPointID string) error
}

// NewMemCheckPointStore 构造进程内检查点存储。
func NewMemCheckPointStore() CheckPoints {
	return &memCheckPointStore{m: map[string][]byte{}}
}

type memCheckPointStore struct {
	mu sync.RWMutex
	m  map[string][]byte
}

func (s *memCheckPointStore) Get(_ context.Context, id string) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.m[id]
	return b, ok, nil
}

func (s *memCheckPointStore) Set(_ context.Context, id string, cp []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = cp
	return nil
}

func (s *memCheckPointStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
	return nil
}

// NewStoreCheckPointStore REQ-204/M39 C1：SQLite 持久化检查点（backend 重启后挂起中断可恢复——
// 消除「重启即丢」诚实边界；checkpoint 表 blob 为 ADK gob 快照）。
func NewStoreCheckPointStore(st *store.Store) CheckPoints {
	return &storeCheckPointStore{st: st}
}

type storeCheckPointStore struct {
	st *store.Store
}

func (s *storeCheckPointStore) Get(ctx context.Context, id string) ([]byte, bool, error) {
	return s.st.GetCheckpoint(id)
}

func (s *storeCheckPointStore) Set(_ context.Context, id string, cp []byte) error {
	return s.st.SetCheckpoint(id, cp)
}

func (s *storeCheckPointStore) Delete(_ context.Context, id string) error {
	return s.st.DeleteCheckpoint(id)
}
