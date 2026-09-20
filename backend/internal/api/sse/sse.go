// Package sse 提供 Server-Sent Events 写出工具。
package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Writer SSE 响应写出器。
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewWriter 构造并写入 SSE 响应头。
func NewWriter(w http.ResponseWriter) (*Writer, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return &Writer{w: w, flusher: flusher}, nil
}

// Event 发送一个具名事件，data 为 JSON。
func (s *Writer) Event(event string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, b); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// RawEvent 发送事件，data 已是 JSON 文本。
func (s *Writer) RawEvent(event string, dataJSON []byte) error {
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, dataJSON); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}
