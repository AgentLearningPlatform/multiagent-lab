// Package ontology 实现本体对接（§6.10）：MCP facade 客户端、双反向代理、
// 运行方案 guide 注入、healthz 探测。主平台不实现本体，只做四个对接点。
package ontology

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/tool"
)

// Service 本体对接服务（P1 主线路径：Runtime Manager facade + 构建平面）。
// D-O15/REQ-110：semantica worker 反代（SemanticaURL :8093）已随「去-semantica 化」移除，
// 消费/审计走主平台自研 KG 与审计端点（api 层 /api/kg、/api/audit）。
type Service struct {
	MCPURL      string        // facade MCP 端点（ONTOLOGY_MCP_URL，默认 http://127.0.0.1:8090/mcp）
	RuntimeURL  string        // 运行平面（RUNTIME_MGR_URL，默认 http://127.0.0.1:8090）
	BuildURL    string        // 构建平面（BUILD_SVC_URL，默认 http://127.0.0.1:8091）
	DialTimeout time.Duration // ONTOLOGY_DIAL_TIMEOUT，默认 3s
}

// NewService 从环境变量构建；始终返回可用实例（不可达由调用方降级）。
func NewService() *Service {
	s := &Service{
		MCPURL:      getenv("ONTOLOGY_MCP_URL", "http://127.0.0.1:8090/mcp"),
		RuntimeURL:  getenv("RUNTIME_MGR_URL", "http://127.0.0.1:8090"),
		BuildURL:    getenv("BUILD_SVC_URL", "http://127.0.0.1:8091"),
		DialTimeout: 3 * time.Second,
	}
	if v := os.Getenv("ONTOLOGY_DIAL_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			s.DialTimeout = d
		}
	}
	return s
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// FetchTools 从 facade 拉 onto_* 工具并转为 Eino 工具（§6.10-1，复用 M9 MCP 装载）。
// 工具名统一加 ontology__ 前缀（与 Agent 级 MCP server 命名规则一致）。
func (s *Service) FetchTools(ctx context.Context) ([]einotool.BaseTool, error) {
	return tool.FetchMCPTools(ctx, "ontology", s.MCPURL, s.DialTimeout)
}

// Guide 运行方案指引（装配时注入 Agent 指引，§6.10-3）。
// Manager 端点：GET /api/runtime-profiles/{id}/guide?ontology_id=…
func (s *Service) FetchGuide(ctx context.Context, profileID, ontologyID string) (string, error) {
	u := fmt.Sprintf("%s/api/runtime-profiles/%s/guide", s.RuntimeURL, url.PathEscape(profileID))
	if ontologyID != "" {
		u += "?ontology_id=" + url.QueryEscape(ontologyID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: s.DialTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("guide 接口返回 %d", resp.StatusCode)
	}
	var out struct {
		Guide string `json:"guide"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		// 非 JSON 时按纯文本处理（Manager 可能直接回 text）
		return "", fmt.Errorf("guide 响应解析失败: %w", err)
	}
	return out.Guide, nil
}

// Reachable healthz 探测：运行平面 /healthz 可达性（1s 预算，healthz 汇总可见 §6.10-4）。
func (s *Service) Reachable(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.RuntimeURL+"/healthz", nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: time.Second}).Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode < 500
}

// BuildProxy 构建平面反代：/api/ontologies* → BUILD_SVC_URL（§6.10-2，同源透传免跨域）。
func (s *Service) BuildProxy() http.Handler { return s.newProxy(s.BuildURL) }

// RuntimeProxy 运行平面反代：/api/runtime-profiles* → RUNTIME_MGR_URL。
func (s *Service) RuntimeProxy() http.Handler { return s.newProxy(s.RuntimeURL) }

func (s *Service) newProxy(target string) http.Handler {
	tu, err := url.Parse(target)
	if err != nil {
		// 配置错误时退化为 502 handler
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeErrJSON(w, http.StatusBadGateway, "本体服务地址配置无效: "+target)
		})
	}
	rp := httputil.NewSingleHostReverseProxy(tu)
	orig := rp.ErrorHandler
	rp.ErrorHandler = func(w http.ResponseWriter, r *http.Request, perr error) {
		if orig != nil {
			orig(w, r, perr)
			return
		}
		writeErrJSON(w, http.StatusBadGateway, "本体服务不可达: "+target)
	}
	// 拨号超时对齐 ONTOLOGY_DIAL_TIMEOUT（§9）
	rp.Transport = &http.Transport{ResponseHeaderTimeout: s.DialTimeout}
	return rp
}

func writeErrJSON(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// Issue 本体挂载降级原因（§6.10-4：dial 失败/方案停止 → ontology.unavailable 事件）。
type Issue struct {
	ProfileID string `json:"profile_id"`
	Reason    string `json:"reason"`
}

// MountOf 判断会话是否挂载本体（runtime_profile_id + ontology_enabled，O-6）。
func MountOf(profileID *string, enabled bool) (string, bool) {
	if !enabled || profileID == nil || *profileID == "" {
		return "", false
	}
	return *profileID, true
}
