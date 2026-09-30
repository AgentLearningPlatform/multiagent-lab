package chat

// REQ-202/M38 ③ verify_on_stop 背压（HumanLayer 实践中回报最高的 Harness 投入）：
// agent.verify_command 非空时，运行 completed 前在授权根目录执行验证命令（sh -c，10s 超时）——
// 退出码非 0 即「背压」：不标记 completed，run.finished reason=verify_failed + run.warning
// 携带输出截断（排障可见）。模型/用户据此修正后再跑。
// 增量注（REQ-202 行内）：「失败自动回传模型继续修」的循环迭代形态留后续增量，本轮为
// 确定性闸门 + 事件透出。

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/xiaoyao/eino-multiagent-lab/backend/internal/fsutil"
)

const verifyTimeout = 10 * time.Second

// RunVerification 执行验证命令：在 dir（授权根；空=进程 cwd）以 sh -c 运行，返回 (输出, 错误)。
// 超时/启动失败都按验证失败处理（fail-closed：闸门不可用不放行）。
func RunVerification(ctx context.Context, cmd, dir string) (string, error) {
	if cmd == "" {
		return "", nil
	}
	cctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()
	full := "sh"
	args := []string{"-c", cmd}
	if d := fsutil.NormalizeDir(dir); d != "" && fsutil.IsAbsDir(d) {
		args = []string{"-lc", fmt.Sprintf("cd %s && %s", shellQuote(d), cmd)}
	}
	out, err := exec.CommandContext(cctx, full, args...).CombinedOutput()
	if cctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("验证命令超时（%s）", verifyTimeout)
	}
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}

// shellQuote 最小 shell 引用（路径来自配置侧校验过的绝对目录，防御性加引号）。
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// truncateRunes 按字符截断（run.warning/事件透出用，防超大输出刷屏）。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…[截断]"
}
