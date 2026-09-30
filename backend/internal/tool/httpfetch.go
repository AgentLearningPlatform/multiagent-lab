package tool

// REQ-202/M38 Harness 执行面：http_fetch 原语。
// 安全边界：仅 GET、仅 http/https、内网/环回地址黑名单（SSRF 粗防）、10s 超时、512KB 读取上限、
// 正文截断 8k 字符；tool_approval=all 的 Agent 经既有审批包装后再执行（审批口径沿用 REQ-14②）。

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

const (
	httpFetchTimeout   = 10 * time.Second
	httpFetchMaxBytes  = 512 << 10 // 512KB
	httpFetchBodyChars = 8000
)

type httpFetchIn struct {
	URL   string            `json:"url" jsonschema:"required"`
	Nonce map[string]string `json:"nonce,omitempty"`
}

type httpFetchOut struct {
	Error   string `json:"error,omitempty"` // 业务错误回执（拒绝/HTTP 4xx/5xx/超时——文本回喂模型，不炸 run）
	Status  int    `json:"status"`
	Body    string `json:"body"`
	CT      string `json:"content_type"`
	Trunc   bool   `json:"truncated"`
}

// blockedHostDenied 内网/环回黑名单（SSRF 粗防：私有段、环回、链路本地、元数据地址）。
func blockedHostDenied(host string) bool {
	h := strings.Trim(host, "[]")
	if h == "localhost" || strings.EqualFold(h, "localhost") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return true
		}
		return false
	}
	// 非字面量主机名：内网域名粗防
	trimmed := strings.TrimSuffix(h, ".")
	return strings.HasSuffix(trimmed, ".local") || strings.HasSuffix(trimmed, ".internal")
}

// NewHTTPFetchTool 抓取公开 HTTP(S) 页面/接口文本（GET；响应正文截断保留头部）。
func NewHTTPFetchTool() (einotool.BaseTool, error) {
	return utils.InferTool("http_fetch",
		"抓取公开 HTTP(S) 地址的响应（仅 GET，10s 超时，正文截断保留前 8k 字符）。用于查公开文档/接口文本；不可访问内网地址。",
		func(_ context.Context, in httpFetchIn) (*httpFetchOut, error) {
			u, err := url.Parse(strings.TrimSpace(in.URL))
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
				return &httpFetchOut{Error: fmt.Sprintf("仅支持 http/https URL: %q", in.URL)}, nil
			}
			if blockedHostDenied(u.Hostname()) {
				return &httpFetchOut{Error: fmt.Sprintf("拒绝访问内网/环回地址: %s", u.Hostname())}, nil
			}
			client := &http.Client{Timeout: httpFetchTimeout,
				CheckRedirect: func(r *http.Request, via []*http.Request) error {
					if len(via) >= 3 {
						return fmt.Errorf("重定向超过 3 次")
					}
					if blockedHostDenied(r.URL.Hostname()) {
						return fmt.Errorf("重定向指向内网/环回地址: %s", r.URL.Hostname())
					}
					return nil
				}}
			resp, err := client.Get(u.String())
			if err != nil {
				return &httpFetchOut{Error: truncateRunes("抓取失败: "+err.Error(), 300)}, nil
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(io.LimitReader(resp.Body, httpFetchMaxBytes+1))
			trunc := false
			if len(b) > httpFetchMaxBytes {
				b = b[:httpFetchMaxBytes]
				trunc = true
			}
			body := string(b)
			if r := []rune(body); len(r) > httpFetchBodyChars {
				body = string(r[:httpFetchBodyChars])
				trunc = true
			}
			if resp.StatusCode >= 400 {
				return &httpFetchOut{Status: resp.StatusCode, Error: fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncateRunes(body, 300))}, nil
			}
			return &httpFetchOut{Status: resp.StatusCode, Body: body, CT: resp.Header.Get("Content-Type"), Trunc: trunc}, nil
		})
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
