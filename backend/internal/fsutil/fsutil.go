// Package fsutil 本地目录路径工具（REQ-101/102 共享）：
// ~ 展开、Windows 盘符识别、目录归一化与安全拼接（目录穿越 / 符号链接逃逸防护）。
// api（文件视图 / 上传下载）与 tool（list_files / read_file / save_file）共用同一实现，
// 避免安全敏感逻辑双份漂移。
package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrPathOutside 路径越界（目录穿越 / 符号链接逃逸）。
var ErrPathOutside = errors.New("path outside root")

// IsWindowsPath 识别 Windows 盘符路径形态（C:/ 或 C:\，大小写盘符均可）。
// 用于跨平台场景：Linux 运行时接受 Windows 客户端提交的本地目录（如挂载盘）。
func IsWindowsPath(p string) bool {
	if len(p) < 3 {
		return false
	}
	c := p[0]
	return (c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z') && p[1] == ':' && (p[2] == '\\' || p[2] == '/')
}

// ExpandHome 展开 ~ 前缀为用户主目录（macOS/Linux 输入习惯；Windows 盘符形态不受影响）。
// 无法取得主目录时原样返回。
func ExpandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
}

// NormalizeDir 用户输入目录 → 运行时格式：去首尾空白、~ 展开、Windows 盘符归一、Clean。
// 返回值不保证绝对路径（调用方按需校验 filepath.IsAbs）。
func NormalizeDir(p string) string {
	d := strings.TrimSpace(p)
	d = ExpandHome(d)
	if IsWindowsPath(d) {
		d = filepath.FromSlash(d)
	}
	return filepath.Clean(d)
}

// Within 判断 p 是否等于 root 或位于 root 之内（词法层面）。
func Within(root, p string) bool {
	root = filepath.Clean(root)
	p = filepath.Clean(p)
	if root == string(os.PathSeparator) {
		return strings.HasPrefix(p, string(os.PathSeparator))
	}
	return p == root || strings.HasPrefix(p, root+string(os.PathSeparator))
}

// SafeJoin 将相对路径 rel 安全拼接到 root：拒绝绝对路径与 ..，并做符号链接逃逸校验。
func SafeJoin(root, rel string) (string, error) {
	root = filepath.Clean(root)
	if rel == "" || rel == "." {
		return root, nil
	}
	if filepath.IsAbs(rel) {
		return "", ErrPathOutside
	}
	cleanRel := filepath.Clean(rel)
	if cleanRel == ".." || strings.HasPrefix(cleanRel, ".."+string(os.PathSeparator)) {
		return "", ErrPathOutside
	}
	full := filepath.Join(root, cleanRel)
	if !Within(root, full) {
		return "", ErrPathOutside
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	if realFull, err := filepath.EvalSymlinks(full); err == nil {
		if !Within(realRoot, realFull) {
			return "", ErrPathOutside
		}
	} else {
		// 目标不存在：校验父目录实路径 + basename，防经符号链接父目录逃逸
		realParent, perr := filepath.EvalSymlinks(filepath.Dir(full))
		if perr != nil {
			return "", ErrPathOutside
		}
		if !Within(realRoot, filepath.Join(realParent, filepath.Base(full))) {
			return "", ErrPathOutside
		}
	}
	return full, nil
}
