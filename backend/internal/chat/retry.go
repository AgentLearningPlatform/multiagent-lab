package chat

// REQ-202/M38 Harness 执行面：幂等重试白名单。
// 现状「单次失败即降级，不重试风暴」（assembler 注）整体保留——白名单只对**幂等且明确可重试**
// 的失败开 N=2 例外：context deadline、连接重置/断开、HTTP 5xx。非白名单错误不重试原样返回。

import (
	"context"
	"errors"
	"strings"
	"time"
)

// retryablePatterns 幂等可重试错误的判定特征（大小写不敏感子串 + errors.Is）。
var retryablePatterns = []string{
	"context deadline exceeded",
	"connection reset",
	"connection refused",
	"broken pipe",
	"eof",
	"timeout",
	"temporarily unavailable",
	"bad gateway",
	"service unavailable",
	"gateway timeout",
	"internal server error",
}

// IsIdempotentRetryable 判定错误是否属幂等可重试白名单。
func IsIdempotentRetryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return errors.Is(err, context.DeadlineExceeded) // 主动取消不重试
	}
	msg := strings.ToLower(err.Error())
	for _, p := range retryablePatterns {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

// DoWithRetry 幂等操作重试：fn 失败且命中白名单时最多再试 retries 次（默认 2），指数退避 200ms 起。
// 首轮失败即返回的整体纪律不破坏——白名单是例外通道，非白名单错误立即返回。
func DoWithRetry(ctx context.Context, retries int, fn func() error) error {
	var err error
	for attempt := 0; ; attempt++ {
		err = fn()
		if err == nil || !IsIdempotentRetryable(err) || attempt >= retries {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(200*(1<<attempt)) * time.Millisecond):
		}
	}
}
