package vocabsearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// REQ-171 P1 零依赖单测：LOV 代理（桩上游）——投影/缓存/上游错误透出。

func TestSearchProjectionAndCache(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Query().Get("q") != "person" || r.URL.Query().Get("type") != "vocabulary" {
			t.Fatalf("上游入参不符: %v", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"results":[{"prefix":"foaf","uri":"http://xmlns.com/foaf/0.1/","title":{"en":"Friend of a Friend"},"description":{"zh":"社交词表","en":"FOAF vocab"}},{"prefix":"schema","uri":"http://schema.org/"}]}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	cards, err := c.Search(context.Background(), "person")
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 || cards[0].Prefix != "foaf" || cards[0].Title != "Friend of a Friend" {
		t.Fatalf("投影不符: %+v", cards)
	}
	// en 优先：description 取 en 而非 zh
	if cards[0].Description != "FOAF vocab" {
		t.Fatalf("应取 en 描述: %q", cards[0].Description)
	}
	// 缺 title 的第二张卡容忍
	if cards[1].Title != "" || cards[1].Prefix != "schema" {
		t.Fatalf("缺字段容错不符: %+v", cards[1])
	}
	// 缓存命中：第二次不打上游
	if _, err := c.Search(context.Background(), "person"); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("应命中缓存（上游仅 1 次），got %d", hits)
	}
}

func TestSearchErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream down"))
	}))
	defer srv.Close()

	c := New(srv.URL)
	if _, err := c.Search(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "LOV 上游 502") {
		t.Fatalf("上游错误应透出: %v", err)
	}
	if _, err := c.Search(context.Background(), "  "); err == nil || !strings.Contains(err.Error(), "q 必填") {
		t.Fatalf("空 q 应报错: %v", err)
	}
}
