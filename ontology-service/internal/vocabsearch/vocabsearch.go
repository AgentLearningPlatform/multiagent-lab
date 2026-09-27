// Package vocabsearch REQ-171 P1 底座薄层：LOV 词表搜索代理（26 号方案 §9 P1 第三子项）。
// LOV（Linked Open Vocabularies）API v2 代理 + 进程内缓存（方案 §6：不落表，进程内即可）；
// 上游不可达时显式报错（前端 P2 展示降级），不做静默兜底。
package vocabsearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultBase = "https://lov.linkeddata.es/dataset/lov/api/v2/search"
	cacheTTL    = 24 * time.Hour
	cacheMax    = 200
	upstreamTO  = 8 * time.Second
)

// Client LOV 搜索代理客户端（Base 可注入供测试桩）。
type Client struct {
	Base  string // 缺省 LOV 官方 v2 search
	HTTP  *http.Client
	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	body []byte
	at   time.Time
}

// New 构造（base 空 = 官方 LOV）。
func New(base string) *Client {
	if strings.TrimSpace(base) == "" {
		base = defaultBase
	}
	return &Client{Base: base, HTTP: &http.Client{Timeout: upstreamTO}, cache: map[string]cacheEntry{}}
}

// Card 检索结果卡片（LOV v2 字段投影，前端展示用；P2 消费）。
type Card struct {
	URI         string `json:"uri"`
	Prefix      string `json:"prefix"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	VocabURI    string `json:"vocab_uri,omitempty"` // 词表主 URI（narrower结果回填）
}

// Search 代理 LOV 关键词搜索，返回词表卡片列表（带进程内缓存）。
func (c *Client) Search(ctx context.Context, q string) ([]Card, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, fmt.Errorf("q 必填")
	}
	if body, ok := c.cached(q); ok {
		return decodeCards(body)
	}
	u := c.Base + "?q=" + url.QueryEscape(q) + "&type=vocabulary"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LOV 上游不可达: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("LOV 上游 %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	c.store(q, body)
	return decodeCards(body)
}

// decodeCards LOV v2 响应 → 卡片列表（results[].preferredNamespace.uri / prefix /
// title.{en,任何语言} / description.{en,...}，容错缺字段）。
func decodeCards(body []byte) ([]Card, error) {
	var raw struct {
		Results []struct {
			Prefix      string            `json:"prefix"`
			URI         string            `json:"uri"`
			Title       map[string]string `json:"title"`
			Description map[string]string `json:"description"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("LOV 响应解析失败: %w", err)
	}
	out := make([]Card, 0, len(raw.Results))
	for _, r := range raw.Results {
		card := Card{URI: r.URI, Prefix: r.Prefix, Title: pickLang(r.Title), Description: pickLang(r.Description)}
		out = append(out, card)
	}
	return out, nil
}

// pickLang 取 en 优先，否则首个非空值。
func pickLang(m map[string]string) string {
	if m == nil {
		return ""
	}
	if v, ok := m["en"]; ok && v != "" {
		return v
	}
	for _, v := range m {
		if v != "" {
			return v
		}
	}
	return ""
}

func (c *Client) cached(q string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache[q]
	if !ok || time.Since(e.at) > cacheTTL {
		return nil, false
	}
	return e.body, true
}

func (c *Client) store(q string, body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.cache) >= cacheMax { // 粗暴容量上限：超限整体重置（缓存非关键路径）
		c.cache = map[string]cacheEntry{}
	}
	c.cache[q] = cacheEntry{body: append([]byte(nil), body...), at: time.Now()}
}
