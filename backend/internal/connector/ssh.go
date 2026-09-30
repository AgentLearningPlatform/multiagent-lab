package connector

// SSHRunner SSH 连接器执行器：x/crypto/ssh 无状态 exec-per-call——每次调用新建连接、
// 执行单条命令、取全输出后关闭（REQ-214 阶段二；交互式 tty/持久会话明确不做）。
// 认证优先私钥（credentials.private_key，可选 passphrase），其次口令（credentials.password）。

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

type SSHRunner struct {
	Host       string
	User       string // 空 = root
	Port       string // 空 = 22
	Password   string
	PrivateKey string
	Passphrase string
}

func (s *SSHRunner) addr() string {
	port := s.Port
	if port == "" {
		port = "22"
	}
	return net.JoinHostPort(s.Host, port)
}

func (s *SSHRunner) user() string {
	if s.User == "" {
		return "root"
	}
	return s.User
}

// client 建立已认证连接。
func (s *SSHRunner) client() (*ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User:            s.user(),
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 学习平台本地面：主机指纹校验不做（诚实标注）
		Timeout:         10 * time.Second,
	}
	if s.PrivateKey != "" {
		var signer ssh.Signer
		var err error
		if s.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(s.PrivateKey), []byte(s.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(s.PrivateKey))
		}
		if err != nil {
			return nil, fmt.Errorf("解析私钥失败: %w", err)
		}
		cfg.Auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
	} else if s.Password != "" {
		cfg.Auth = []ssh.AuthMethod{ssh.Password(s.Password)}
	} else {
		return nil, fmt.Errorf("未配置认证凭据（private_key 或 password）")
	}
	return ssh.Dial("tcp", s.addr(), cfg)
}

// Exec 执行单条命令（exec-per-call），返回合并输出；非零退出返回 *exitError。
func (s *SSHRunner) Exec(ctx context.Context, command string, timeoutSec int) (string, error) {
	if strings.TrimSpace(command) == "" {
		return "", fmt.Errorf("command 必填")
	}
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	if timeoutSec > 300 {
		timeoutSec = 300
	}
	cli, err := s.client()
	if err != nil {
		return "", err
	}
	defer cli.Close()

	sess, err := cli.NewSession()
	if err != nil {
		return "", fmt.Errorf("新建会话失败: %w", err)
	}
	defer sess.Close()

	var outBuf, errBuf strings.Builder
	sess.Stdout = &outBuf
	sess.Stderr = &errBuf
	done := make(chan error, 1)
	go func() { done <- sess.Run(command) }()

	timer := time.NewTimer(time.Duration(timeoutSec) * time.Second)
	defer timer.Stop()
	outSoFar := func() string { return strings.TrimRight(outBuf.String(), "\n") }
	select {
	case err := <-done:
		out := outSoFar()
		if err != nil {
			if ee, ok := err.(*ssh.ExitError); ok {
				return out, &exitError{Code: ee.ExitStatus(), Stderr: strings.TrimSpace(errBuf.String())}
			}
			return out, err
		}
		return out, nil
	case <-timer.C:
		_ = sess.Signal(ssh.SIGKILL)
		return outSoFar(), fmt.Errorf("命令超时（%ds）", timeoutSec)
	case <-ctx.Done():
		return outSoFar(), ctx.Err()
	}
}

// Probe 连接测试：拨号 + 认证 + echo 探针。
func (s *SSHRunner) Probe(ctx context.Context) (string, error) {
	cli, err := s.client()
	if err != nil {
		return "", err
	}
	defer cli.Close()
	sess, err := cli.NewSession()
	if err != nil {
		return "", err
	}
	defer sess.Close()
	out, err := sess.Output("echo ok")
	if err != nil {
		return "", err
	}
	if !strings.Contains(string(out), "ok") {
		return "", fmt.Errorf("探针回包异常")
	}
	return "认证成功（" + s.user() + "@" + s.addr() + "）", nil
}
