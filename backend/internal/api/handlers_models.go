package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/secrets"
	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/store"
)

// ---- Model Connections ----

func (s *Server) listConnections(w http.ResponseWriter, r *http.Request) {
	conns, err := s.Store.ListConnections()
	if err != nil {
		writeErr(w, err)
		return
	}
	if conns == nil {
		conns = []*store.ModelConnection{}
	}
	writeJSON(w, http.StatusOK, conns)
}

func (s *Server) createConnection(w http.ResponseWriter, r *http.Request) {
	var c store.ModelConnection
	if err := decodeJSON(r, &c); err != nil {
		writeErr(w, err)
		return
	}
	enc, err := s.resolveConnKey(&c)
	if err != nil {
		writeErr(w, err)
		return
	}
	created, err := s.Store.CreateConnection(&c, enc)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// resolveConnKey 解析连接写入的 Key 密文：
//  1. 携带明文 api_key → 加密；
//  2. 未携带明文但指定 copy_key_from → 复用源连接已存密文（供应商 → 多模型共享 Key）；
//  3. 两者皆空 → 返回 nil（创建 = 无 Key；更新 = 保留原 Key）。
func (s *Server) resolveConnKey(c *store.ModelConnection) ([]byte, error) {
	switch {
	case c.APIKey != "":
		b, err := s.Box.Encrypt(c.APIKey)
		if err != nil {
			return nil, err
		}
		c.APIKeyHint = secrets.MaskKey(c.APIKey)
		return b, nil
	case c.CopyKeyFrom != "":
		src, err := s.Store.GetConnectionRecord(c.CopyKeyFrom)
		if err != nil {
			return nil, err
		}
		c.APIKeyHint = src.Conn.APIKeyHint
		return src.Encrypted, nil
	default:
		return nil, nil
	}
}

func (s *Server) getConnection(w http.ResponseWriter, r *http.Request) {
	c, err := s.Store.GetConnection(r.PathValue("id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) updateConnection(w http.ResponseWriter, r *http.Request) {
	var c store.ModelConnection
	if err := decodeJSON(r, &c); err != nil {
		writeErr(w, err)
		return
	}
	id := r.PathValue("id")
	enc, err := s.resolveConnKey(&c)
	if err != nil {
		writeErr(w, err)
		return
	}
	c.ID = id
	updated, err := s.Store.UpdateConnection(&c, enc)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) deleteConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// 保护：有 Agent 引用时拒绝删除
	n, err := s.Store.CountAgentsUsingConn(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if n > 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": fmt.Sprintf("该连接正被 %d 个智能体使用，请先解除引用", n)})
		return
	}
	if err := s.Store.DeleteConnection(id); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func (s *Server) setDefaultConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := s.Store.GetConnection(id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.Store.SetDefaultConnection(id, c.ConnType); err != nil {
		writeErr(w, err)
		return
	}
	updated, _ := s.Store.GetConnection(id)
	writeJSON(w, http.StatusOK, updated)
}

// ConnTestInput 测试连接输入；不携带 id 时表示测试未保存配置。
type ConnTestInput struct {
	ID       string `json:"id,omitempty"`
	ConnType string `json:"conn_type,omitempty"`
	BaseURL  string `json:"base_url,omitempty"`
	Model    string `json:"model_name,omitempty"`
	APIKey   string `json:"api_key,omitempty"`
}

// testConnection 测试模型连通性：chat 发最小 completion；embedding 发 embeddings 请求。
func (s *Server) testConnection(w http.ResponseWriter, r *http.Request) {
	var in ConnTestInput
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	// 传 id：用已存配置（key 走解密）
	if in.ID != "" {
		rec, err := s.Store.GetConnectionRecord(in.ID)
		if err != nil {
			writeErr(w, err)
			return
		}
		key, err := s.Box.Decrypt(rec.Encrypted)
		if err != nil {
			writeErr(w, err)
			return
		}
		in.ConnType = rec.Conn.ConnType
		in.BaseURL = rec.Conn.BaseURL
		in.Model = rec.Conn.ModelName
		in.APIKey = key
	}
	if in.BaseURL == "" || in.Model == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "base_url 与 model_name 必填"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	start := time.Now()
	var errMsg string
	switch in.ConnType {
	case "embedding":
		errMsg = probeEmbedding(ctx, in.BaseURL, in.Model, in.APIKey)
	default:
		errMsg = probeChat(ctx, in.BaseURL, in.Model, in.APIKey)
	}
	elapsed := time.Since(start).Milliseconds()
	if errMsg != "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": errMsg, "elapsed_ms": elapsed})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "elapsed_ms": elapsed})
}

// probeChat 用最小请求测 chat 连通（回复长度限制 8 token 级别）。
func probeChat(ctx context.Context, baseURL, model, key string) string {
	out, err := simpleChatCompletion(ctx, baseURL, model, key, "ping，请回复 ok")
	if err != nil {
		return err.Error()
	}
	_ = out
	return ""
}

// simpleChatCompletion 直接走 /chat/completions（与 openai_compat 协议一致，轻量无 SDK 依赖）。
func simpleChatCompletion(ctx context.Context, baseURL, model, key, user string) (string, error) {
	body := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": user},
		},
		"max_tokens": 16,
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, joinURL(baseURL, "/chat/completions"), bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("HTTP %d, bad response: %v", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if out.Error != nil {
			msg += ": " + out.Error.Message
		}
		return "", fmt.Errorf("%s", msg)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("empty choices")
	}
	return out.Choices[0].Message.Content, nil
}

// probeEmbedding 测 embeddings 连通。
func probeEmbedding(ctx context.Context, baseURL, model, key string) string {
	body := map[string]any{"model": model, "input": "ping"}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, joinURL(baseURL, "/embeddings"), bytes.NewReader(b))
	if err != nil {
		return err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err.Error()
	}
	defer resp.Body.Close()
	var out struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		if out.Error != nil {
			msg += ": " + out.Error.Message
		}
		return msg
	}
	return ""
}

func joinURL(base, path string) string {
	for len(base) > 0 && base[len(base)-1] == '/' {
		base = base[:len(base)-1]
	}
	return base + path
}
