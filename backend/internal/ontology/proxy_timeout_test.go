package ontology

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 回归（2026-09-27 开发者报障「OntoChat 发送消息返回本体服务不可达」）：
// 反代此前误把 DialTimeout(3s) 填进 ResponseHeaderTimeout——OntoChat turn 为同步 LLM
// 生成端点（响应头在生成完成后才写，可达分钟级），慢上游一律被误报 502 不可达。
// 修复后：拨号超时只约束建连；响应头超时独立（默认不限）。

func TestBuildProxySlowUpstreamStillServed(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1200 * time.Millisecond) // 远超拨号超时（模拟 LLM 生成轮）
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"reply":"ok"}`))
	}))
	defer slow.Close()

	s := &Service{BuildURL: slow.URL, DialTimeout: 200 * time.Millisecond} // 拨号毫秒级
	h := s.BuildProxy()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/ontochat/sessions/x/turn", strings.NewReader(`{"text":"hi"}`))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("慢上游应正常透传，得到 %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"reply":"ok"`) {
		t.Errorf("响应体不符: %s", rec.Body.String())
	}
}

func TestBuildProxyUnreachableGives502WithDetail(t *testing.T) {
	s := &Service{BuildURL: "http://127.0.0.1:1", DialTimeout: 200 * time.Millisecond}
	h := s.BuildProxy()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/ontochat/sessions", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("不可达应 502，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "本体服务不可达") || !strings.Contains(rec.Body.String(), "connect") {
		t.Errorf("502 信息应含「本体服务不可达」与底层错误细节: %s", rec.Body.String())
	}
}
