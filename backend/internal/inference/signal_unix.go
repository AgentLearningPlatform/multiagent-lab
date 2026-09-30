//go:build !windows

package inference

import "os"

// quitSignal 关闭阶梯第二档信号（Windows 无 SIGTERM，编译隔离）。
func quitSignal() os.Signal { return os.Interrupt }
