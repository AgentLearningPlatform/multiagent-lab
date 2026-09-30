package chat

import (
	"context"
	"errors"
	"testing"
)

func TestIsIdempotentRetryable(t *testing.T) {
	cases := map[error]bool{
		context.DeadlineExceeded:                     true,
		errors.New("connection reset by peer"):       true,
		errors.New("HTTP 503 Service Unavailable"):   true,
		errors.New("read eof"):                       true,
		context.Canceled:                             false,
		errors.New("invalid argument"):               false,
		errors.New("permission denied"):              false,
		nil:                                          false,
	}
	for err, want := range cases {
		if got := IsIdempotentRetryable(err); got != want {
			t.Fatalf("%v: 期望 %v got %v", err, want, got)
		}
	}
}

func TestDoWithRetry(t *testing.T) {
	// 白名单错误重试后成功
	n := 0
	err := DoWithRetry(context.Background(), 2, func() error {
		n++
		if n < 2 {
			return errors.New("connection reset by peer")
		}
		return nil
	})
	if err != nil || n != 2 {
		t.Fatalf("重试后应成功: %v n=%d", err, n)
	}
	// 非白名单错误不重试
	n = 0
	err = DoWithRetry(context.Background(), 2, func() error {
		n++
		return errors.New("permission denied")
	})
	if err == nil || n != 1 {
		t.Fatalf("非白名单不应重试: %v n=%d", err, n)
	}
	// 超过上限
	n = 0
	_ = DoWithRetry(context.Background(), 2, func() error {
		n++
		return context.DeadlineExceeded
	})
	if n != 3 { // 首轮 + 2 重试
		t.Fatalf("应恰好重试 2 次: n=%d", n)
	}
}
