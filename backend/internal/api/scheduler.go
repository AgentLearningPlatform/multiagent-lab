package api

// REQ-204/M39 C5：对话级定时续跑调度器（学习尺度最小闭环）。
// 用户显式为会话设定「每 N 分钟推进一次、最多 M 次」，到点自动发起一次 Run（固定续跑指令，
// 按 todo_write 任务清单推进一个有界增量并更新清单）；达到 max_runs 自动摘除。
// Cap 纪律（38 号 C5）：interval ≥1 分钟、maxRuns ≤50 硬上限——防 Token Blowout。
// REQ-224/M52：调度状态出进程入 DB（迁移 038 conversation_schedule）——创建/触发计数/摘除
// 全部落库，backend 重启后 LoadSchedules 重新装配定时器（18 v1.91「重启失效」诚实边界收口）；
// 内存 map 仅存运行期句柄（ticker/stop），DB 行为唯一事实源。运行中的会话（活跃 Run）到点跳过本轮不排队。

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
}

type scheduler struct {
	mu   sync.Mutex
	api  *Server
	conf map[string]*scheduleEntry // conversationID -> 运行期句柄（事实源在 conversation_schedule 表）
}

func newScheduler(api *Server) *scheduler {
	return &scheduler{api: api, conf: map[string]*scheduleEntry{}}
}

// load 启动装配：按 DB 活跃行重新武装定时器（重启续跑收口）。
func (sc *scheduler) load() {
	rows, err := sc.api.Store.ListSchedules()
	if err != nil {
		log.Printf("[scheduler] 启动装配读取失败：%v", err)
		return
	}
	armed := 0
	for _, r := range rows {
		if r.Done >= r.MaxRuns { // 已达 cap 的残留行：清掉
			_ = sc.api.Store.DeleteSchedule(r.ConversationID)
			continue
		}
		sc.arm(r.ConversationID, r.IntervalMinutes, r.MaxRuns)
		armed++
	}
	if armed > 0 {
		log.Printf("[scheduler] 重启装配 %d 条定时续跑调度", armed)
	}
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
	if err := s.Store.UpsertSchedule(convID, in.IntervalMinutes, in.MaxRuns); err != nil {
		writeErr(w, err)
		return
	}
	s.sched.arm(convID, in.IntervalMinutes, in.MaxRuns) // 重设 = 覆盖句柄
	writeJSON(w, http.StatusOK, map[string]any{"conversation_id": convID, "interval_minutes": in.IntervalMinutes, "max_runs": in.MaxRuns, "done": 0})
}

func (s *Server) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	convID := r.PathValue("id")
	s.sched.disarm(convID)
	if err := s.Store.DeleteSchedule(convID); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

func (s *Server) listSchedules(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.Store.ListSchedules()
	if err != nil {
		writeErr(w, err)
		return
	}
	out := []map[string]any{}
	for _, r := range rows {
		out = append(out, map[string]any{"conversation_id": r.ConversationID, "interval_minutes": r.IntervalMinutes, "max_runs": r.MaxRuns, "done": r.Done})
	}
	writeJSON(w, http.StatusOK, out)
}

// arm 武装（或重设）一条定时器句柄；DB 行由调用方先落。
func (sc *scheduler) arm(convID string, intervalMin, maxRuns int) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if old, ok := sc.conf[convID]; ok {
		close(old.Stop)
		old.Ticker.Stop()
	}
	e := &scheduleEntry{
		Interval: time.Duration(intervalMin) * time.Minute,
		MaxRuns:  maxRuns,
		Stop:     make(chan struct{}),
	}
	e.Ticker = time.NewTicker(e.Interval)
	sc.conf[convID] = e
	go sc.loop(convID, e)
}

// disarm 解除句柄（DB 行由调用方处置）。
func (sc *scheduler) disarm(convID string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if e, ok := sc.conf[convID]; ok {
		close(e.Stop)
		e.Ticker.Stop()
		delete(sc.conf, convID)
	}
}

func (sc *scheduler) loop(convID string, e *scheduleEntry) {
	for {
		select {
		case <-e.Stop:
			return
		case <-e.Ticker.C:
			done, err := sc.api.Store.IncrementScheduleDone(convID)
			if err != nil { // 行已被并发摘除：句柄退役
				sc.disarm(convID)
				return
			}
			sc.fire(convID, done, e.MaxRuns)
			if done >= e.MaxRuns { // cap 达到：fire 后立即摘除（Cap Before You Ship）
				sc.disarm(convID)
				_ = sc.api.Store.DeleteSchedule(convID)
				return
			}
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
