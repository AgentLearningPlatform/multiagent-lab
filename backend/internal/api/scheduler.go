package api

// REQ-204/M39 C5：对话级定时续跑调度器（学习尺度最小闭环）。
// 用户显式为会话设定「每 N 分钟推进一次、最多 M 次」，到点自动发起一次 Run（固定续跑指令，
// 按 todo_write 任务清单推进一个有界增量并更新清单）；达到 max_runs 自动摘除。
// Cap 纪律（38 号 C5）：interval ≥1 分钟、maxRuns ≤50 硬上限——防 Token Blowout。
// 诚实边界：调度器为**进程内**形态（学习尺度单机），backend 重启后调度配置失效（不持久）——
// 持久化调度（跨重启）留后续增量；运行中的会话（活跃 Run）到点跳过本轮不排队。

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
)

const (
	scheduleMinIntervalMin = 1
	scheduleMaxRuns        = 50
	scheduleRunInput       = "定时续跑：请按当前任务清单（若已用 todo_write 建立清单则按清单推进一个有界增量并更新状态；若无清单则推进当前任务的一小步并汇报）。回复保持简短。"
)

type scheduleEntry struct {
	Ticker   *time.Ticker
	Stop     chan struct{}
	Interval time.Duration
	MaxRuns  int
	Done     int
}

type scheduler struct {
	mu   sync.Mutex
	api  *Server
	conf map[string]*scheduleEntry // conversationID -> entry
}

func newScheduler(api *Server) *scheduler {
	return &scheduler{api: api, conf: map[string]*scheduleEntry{}}
}

type schedulePayload struct {
	IntervalMinutes int `json:"interval_minutes"`
	MaxRuns         int `json:"max_runs"`
}

func (s *Server) createSchedule(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	var in schedulePayload
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if in.IntervalMinutes < scheduleMinIntervalMin || in.MaxRuns < 1 || in.MaxRuns > scheduleMaxRuns {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("interval_minutes ≥%d 且 1≤max_runs≤%d", scheduleMinIntervalMin, scheduleMaxRuns)})
		return
	}
	if _, err := s.Store.GetConversation(convID); err != nil {
		writeErr(w, err)
		return
	}
	s.sched.mu.Lock()
	defer s.sched.mu.Unlock()
	if old, ok := s.sched.conf[convID]; ok { // 重设 = 覆盖
		close(old.Stop)
		old.Ticker.Stop()
	}
	entry := &scheduleEntry{
		Interval: time.Duration(in.IntervalMinutes) * time.Minute,
		MaxRuns:  in.MaxRuns,
		Stop:     make(chan struct{}),
	}
	entry.Ticker = time.NewTicker(entry.Interval)
	s.sched.conf[convID] = entry
	go s.sched.loop(convID, entry)
	writeJSON(w, http.StatusOK, map[string]any{"conversation_id": convID, "interval_minutes": in.IntervalMinutes, "max_runs": in.MaxRuns, "done": 0})
}

func (s *Server) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	s.sched.mu.Lock()
	defer s.sched.mu.Unlock()
	if e, ok := s.sched.conf[convID]; ok {
		close(e.Stop)
		e.Ticker.Stop()
		delete(s.sched.conf, convID)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) listSchedules(w http.ResponseWriter, _ *http.Request) {
	s.sched.mu.Lock()
	defer s.sched.mu.Unlock()
	out := []map[string]any{}
	for id, e := range s.sched.conf {
		out = append(out, map[string]any{"conversation_id": id, "interval_minutes": int(e.Interval / time.Minute), "max_runs": e.MaxRuns, "done": e.Done})
	}
	writeJSON(w, http.StatusOK, out)
}

func (sc *scheduler) loop(convID string, e *scheduleEntry) {
	for {
		select {
		case <-e.Stop:
			return
		case <-e.Ticker.C:
			sc.mu.Lock()
			if e.Done >= e.MaxRuns { // cap 达到：自动摘除（Cap Before You Ship）
				close(e.Stop)
				e.Ticker.Stop()
				delete(sc.conf, convID)
				sc.mu.Unlock()
				return
			}
			e.Done++
			done := e.Done
			sc.mu.Unlock()
			sc.fire(convID, done, e.MaxRuns)
			sc.mu.Lock()
			if cur, ok := sc.conf[convID]; ok && cur == e && e.Done >= e.MaxRuns { // cap 达到：fire 后立即摘除
				close(e.Stop)
				e.Ticker.Stop()
				delete(sc.conf, convID)
			}
			sc.mu.Unlock()
		}
	}
}

// fire 触发一次续跑 Run（跳过活跃会话；错误仅日志——调度器不重试风暴）。
func (sc *scheduler) fire(convID string, done, maxRuns int) {
	s := sc.api
	if s.Chat.Running(convID) {
		log.Printf("[scheduler] %s 活跃运行中，本轮跳过（done=%d/%d）", convID[:8], done, maxRuns)
		return
	}
	conv, err := s.Store.GetConversation(convID)
	if err != nil {
		return
	}
	if conv.AgentID == nil || *conv.AgentID == "" {
		return
	}
	agent, err := s.Store.GetAgent(*conv.AgentID)
	if err != nil {
		return
	}
	runID := fmt.Sprintf("sched-%d", time.Now().UnixNano())
	log.Printf("[scheduler] %s 定时续跑第 %d/%d 次", convID[:8], done, maxRuns)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	_, _ = s.Chat.Run(ctx, conv, agent, runID, scheduleRunInput, 0, false, func(*chat.Event) {})
}
