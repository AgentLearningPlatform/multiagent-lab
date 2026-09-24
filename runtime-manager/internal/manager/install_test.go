package manager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-146 下载机制零网络单测（httptest 模拟 release 服务器）。

func TestDownloadToSuccess(t *testing.T) {
	// 2MB 合法内容（超过 1MB 下限）
	payload := strings.Repeat("oxigraph-bin-payload-", 100<<10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "oxigraph")
	if err := downloadTo(context.Background(), srv.URL, target, 1<<20); err != nil {
		t.Fatalf("downloadTo: %v", err)
	}
	st, err := os.Stat(target)
	if err != nil {
		t.Fatalf("target missing: %v", err)
	}
	if st.Size() != int64(len(payload)) {
		t.Fatalf("size = %d, want %d", st.Size(), len(payload))
	}
	if st.Mode()&0o111 == 0 {
		t.Fatal("执行位未设置")
	}
	if _, err := os.Stat(target + ".download"); !os.IsNotExist(err) {
		t.Fatal("temp 文件应已原子替换移除")
	}
}

func TestDownloadToFailures(t *testing.T) {
	// 404：不落盘
	srv404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv404.Close()
	dir := t.TempDir()
	target := filepath.Join(dir, "oxigraph")
	if err := downloadTo(context.Background(), srv404.URL, target, 1); err == nil {
		t.Fatal("404 应报错")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("404 不应落盘")
	}

	// 内容过短（低于 minBytes）：防错误页/截断落盘
	srvShort := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("short"))
	}))
	defer srvShort.Close()
	if err := downloadTo(context.Background(), srvShort.URL, target, 1<<20); err == nil {
		t.Fatal("短内容应报错")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("短内容不应落盘")
	}

	// 服务端中断流：报错且清理 temp
	srvCut := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1048576")
		_, _ = w.Write([]byte(strings.Repeat("x", 1024)))
		// 提前断开
		panic(http.ErrAbortHandler)
	}))
	defer srvCut.Close()
	if err := downloadTo(context.Background(), srvCut.URL, target, 1<<20); err == nil {
		t.Fatal("中断流应报错")
	}
	if _, err := os.Stat(target + ".download"); !os.IsNotExist(err) {
		t.Fatal("中断后 temp 应清理")
	}
}
