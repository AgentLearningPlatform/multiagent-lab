// REQ-209/M42 远程部署访问凭证：单一共享静态 token 做非 loopback 访问的粗粒度准入。
//
// 与 Q-5「不做登录/账户体系」不同层：无用户/角色/会话隔离，纯平台级准入（10 号 §11.1 拆出）。
// - PLATFORM_TOKEN 未设置/为空 = 不启用，本机开发零影响（原有行为，零中间件开销）；
// - 启用后仅管 /api/* 数据面；/healthz（存活探测）、/mcp（REQ-131 自带 Bearer 鉴权）、
//   静态资源（无数据）放行；
// - loopback 豁免：RemoteAddr 为回环地址**且无代理转发头**（同机反代会带 X-Forwarded-For，
//   此时请求源自远程客户端，须持凭证——防「反代同机部署令 token 形同虚设」的豁免漏洞）；
// - 凭证来源：Authorization: Bearer / X-Platform-Token / ?access_token=（EventSource 兼容）；
//   比较用 subtle.ConstantTimeCompare 防时序侧信道。
package main

import (
	"crypto/subtle"
	"net"
	"net/http"
	"os"
	"strings"
)

func accessToken() string { return strings.TrimSpace(os.Getenv("PLATFORM_TOKEN")) }

func accessAuth(next http.Handler) http.Handler {
	token := accessToken()
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") { // 仅 /api/* 数据面（见包注）
			next.ServeHTTP(w, r)
			return
		}
		if loopbackDirect(r) { // 本机直连豁免（无代理转发头）
			next.ServeHTTP(w, r)
			return
		}
		if subtle.ConstantTimeCompare([]byte(presentedToken(r)), []byte(token)) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="platform"`)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"需要访问凭证：本平台已启用 PLATFORM_TOKEN（非本机访问须携带 Bearer 凭证 / X-Platform-Token 头或 ?access_token= 参数）"}`))
	})
}

// loopbackDirect 回环直连判定：RemoteAddr 为 loopback 且未经过代理转发（无 X-Forwarded-For /
// X-Real-IP 头）。转发头由反代注入，出现即视为远程来源，不豁免。
func loopbackDirect(r *http.Request) bool {
	if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Real-IP") != "" {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(strings.TrimSpace(host))
	return ip != nil && ip.IsLoopback()
}

// presentedToken 提取请求凭证（Bearer 头 > X-Platform-Token 头 > access_token 查询参数）。
func presentedToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	}
	if h := strings.TrimSpace(r.Header.Get("X-Platform-Token")); h != "" {
		return h
	}
	return strings.TrimSpace(r.URL.Query().Get("access_token"))
}
