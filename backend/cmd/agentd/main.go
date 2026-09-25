// agentd 沙箱入口（方案 §6.3，M10）：agent-runtime 镜像的容器内主进程。
//
// 启动时凭一次性 token 从主平台拉取完整配置（manifest：agent/模型连接/技能），
// 写入容器内内存 SQLite，再用与主平台同一份 Assembler / chat.Service 完成装配与运行，
// 保证 inprocess 与沙箱行为一致（方案风险：装配代码漂移）。
//
// 环境变量：
//
//	AGENT_ID        必填，沙箱承载的 Agent ID
//	PLATFORM_URL    必填，主平台地址（如 http://host.docker.internal:8080）
//	MANIFEST_TOKEN  必填，一次性配置下发 token（容器启动时注入）
//	ADDR            监听地址，默认 :8080
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/chat"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/ontology"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/skill"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/tool"
)

// manifest 下发载荷（与主平台 internal/api 一致）。
type manifestModelConn struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ConnType  string `json:"conn_type"`
	Protocol  string `json:"protocol"`
	BaseURL   string `json:"base_url"`
	ModelName string `json:"model_name"`
	Enabled   bool   `json:"enabled"`
	IsDefault bool   `json:"is_default"`
	APIKey    string `json:"api_key,omitempty"`
}

type manifest struct {
	Agent      *store.Agent        `json:"agent"`
	ModelConns []manifestModelConn `json:"model_conns,omitempty"`
	Skills     []*store.Skill      `json:"skills,omitempty"`
}

// runRequest 主平台转发的运行请求。
type runRequest struct {
	Input            string          `json:"input"`
	RunID            string          `json:"run_id"`
	History          []store.Message `json:"history,omitempty"` // 不含最后一条 user（agentd 端 Run 会存 input）
	RuntimeProfileID *string         `json:"runtime_profile_id,omitempty"`
	OntologyEnabled  bool            `json:"ontology_enabled,omitempty"`
	DebugLevel       int             `json:"debug_level,omitempty"` // M17 阶段二：调试档经沙箱请求透传
	DebugPersist     bool            `json:"debug_persist,omitempty"`
}

type agentd struct {
	agentID string
	svc     *chat.Service
	agent   *store.Agent
}

func main() {
	agentID := os.Getenv("AGENT_ID")
	platformURL := strings.TrimRight(os.Getenv("PLATFORM_URL"), "/")
	token := os.Getenv("MANIFEST_TOKEN")
	if agentID == "" || platformURL == "" || token == "" {
		log.Fatal("[agentd] AGENT_ID / PLATFORM_URL / MANIFEST_TOKEN required")
	}
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}

	mf, err := fetchManifest(platformURL, agentID, token)
	if err != nil {
		log.Fatalf("[agentd] fetch manifest: %v", err)
	}

	// 内存 SQLite + 临时密钥文件（容器内自加密，密文不出容器）
	st, err := store.Open(":memory:")
	if err != nil {
		log.Fatalf("[agentd] open memory store: %v", err)
	}
	keyFile, err := os.CreateTemp("", "agentd-key-*")
	if err != nil {
		log.Fatalf("[agentd] create key file: %v", err)
	}
	if _, err := keyFile.WriteString(randomKey()); err != nil {
		log.Fatalf("[agentd] write key file: %v", err)
	}
	_ = keyFile.Close()
	box, err := secrets.LoadKeyFile(keyFile.Name())
	if err != nil {
		log.Fatalf("[agentd] load key: %v", err)
	}

	// 写入 manifest 数据：agent、模型连接、技能
	for _, mc := range mf.ModelConns {
		enc, _ := box.Encrypt(mc.APIKey)
		c := &store.ModelConnection{
			ID: mc.ID, Name: mc.Name, ConnType: mc.ConnType, Protocol: mc.Protocol,
			BaseURL: mc.BaseURL, ModelName: mc.ModelName,
			APIKeyHint: maskOf(mc.APIKey), Enabled: mc.Enabled, IsDefault: mc.IsDefault,
		}
		if _, err := st.CreateConnection(c, enc); err != nil {
			log.Fatalf("[agentd] seed connection %s: %v", mc.ID, err)
		}
	}
	for _, sk := range mf.Skills {
		if _, err := st.CreateSkill(sk); err != nil {
			log.Printf("[agentd] seed skill %s: %v", sk.ID, err)
		}
	}
	if _, err := st.CreateAgent(mf.Agent); err != nil {
		log.Fatalf("[agentd] seed agent: %v", err)
	}

	// 与主平台同一套装配与运行代码（§6.3 一致性要求）
	// 沙箱内即运行终点：agent 的 docker 后端标注在容器内不再二次分发
	if mf.Agent != nil {
		mf.Agent.RuntimeBackend = "inprocess"
	}
	reg := tool.NewRegistry()
	if err := tool.RegisterBuiltin(reg); err != nil {
		log.Fatalf("[agentd] register builtin tools: %v", err)
	}
	asm := &chat.Assembler{
		Store:    st,
		Box:      box,
		Tools:    reg,
		Composer: &skill.Composer{Store: st},
		Ontology: ontology.NewService(), // 容器内 env 缺省时自动降级
	}
	ad := &agentd{agentID: agentID, svc: chat.NewService(st, asm, nil), agent: mf.Agent}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("POST /run", ad.handleRun)

	log.Printf("[agentd] ready: agent=%s addr=%s model_conns=%d skills=%d",
		agentID, addr, len(mf.ModelConns), len(mf.Skills))
	srv := &http.Server{Addr: addr, Handler: mux}
	log.Fatal(srv.ListenAndServe())
}

// handleRun POST /run：与主平台事件协议一致的 SSE 流。
func (a *agentd) handleRun(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "decode run request: "+err.Error(), http.StatusBadRequest)
		return
	}
	sw, err := sseWriter(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	convID := store.NewID()
	conv := &store.Conversation{
		ID: convID, Scope: "agent", AgentID: &a.agentID,
		RuntimeProfileID: req.RuntimeProfileID, OntologyEnabled: req.OntologyEnabled,
	}
	// 会话先落库（message.conversation_id 外键），单次请求一个内存会话
	if _, err := a.svc.Store.CreateConversation(conv); err != nil {
		log.Printf("[agentd] create sandbox conversation: %v", err) // 10a：错误可见（此前静默，仅 superfluous WriteHeader 线索）
		http.Error(w, "create sandbox conversation: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// 历史消息写入内存库（不含最后一条 user；Run 内部会存 input）
	for i := range req.History {
		m := req.History[i]
		m.ID = store.NewID()
		m.ConversationID = convID
		if _, err := a.svc.Store.InsertMessage(&m); err != nil {
			log.Printf("[agentd] seed history message: %v", err)
		}
	}

	runID := req.RunID
	if runID == "" {
		runID = store.NewID()
	}
	emit := func(ev *chat.Event) {
		_ = sw(ev.Type, ev) // SSE event 名 = 平台事件类型（与主平台协议一致）
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	res, rerr := a.svc.Run(ctx, conv, a.agent, runID, req.Input, req.DebugLevel, req.DebugPersist, emit) // M17 阶段二：调试档与入库开关经沙箱请求透传
	if rerr != nil {
		_ = sw("event", chat.NewErrorEvent(runID, "run_failed", rerr.Error()))
	}
	log.Printf("[agentd] run %s finished: stopped=%v err=%q", runID, res != nil && res.Stopped, mapErr(res))
}

func mapErr(res *chat.RunResult) string {
	if res == nil {
		return ""
	}
	return res.Error
}

func fetchManifest(platformURL, agentID, token string) (*manifest, error) {
	cli := &http.Client{Timeout: 15 * time.Second}
	resp, err := cli.Get(fmt.Sprintf("%s/api/internal/agents/%s/manifest?token=%s", platformURL, agentID, token))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest http %d", resp.StatusCode)
	}
	var mf manifest
	if err := json.NewDecoder(resp.Body).Decode(&mf); err != nil {
		return nil, err
	}
	return &mf, nil
}

func randomKey() string {
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(time.Now().UnixNano() >> uint(i%16))
	}
	return string(b)
}

func maskOf(key string) string {
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}

// sseWriter 轻量 SSE 输出（避免依赖 internal/api）。
type sseFunc func(event string, data any) error

func sseWriter(w http.ResponseWriter) (sseFunc, error) {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming unsupported")
	}
	f.Flush()
	return func(event string, data any) error {
		b, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return err
		}
		f.Flush()
		return nil
	}, nil
}
