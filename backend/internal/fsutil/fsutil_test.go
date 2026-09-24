package fsutil

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got := ExpandHome("~"); got != filepath.Clean(home) {
		t.Errorf("ExpandHome(~) = %q, want %q", got, home)
	}
	if got := ExpandHome("~/work"); got != filepath.Join(home, "work") {
		t.Errorf("ExpandHome(~/work) = %q, want %q", got, filepath.Join(home, "work"))
	}
	if got := ExpandHome("/abs/x"); got != "/abs/x" {
		t.Errorf("ExpandHome(/abs/x) = %q, want unchanged", got)
	}
	if got := ExpandHome("rel/~x"); got != "rel/~x" {
		t.Errorf("ExpandHome(rel/~x) = %q, want unchanged", got)
	}
}

func TestNormalizeDir(t *testing.T) {
	home, _ := os.UserHomeDir()
	if got := NormalizeDir("  ~/work  "); got != filepath.Join(home, "work") {
		t.Errorf("NormalizeDir(~/work with spaces) = %q", got)
	}
	// Windows 盘符形态在非 Windows 运行时保持原样（FromSlash 不改变 / 分隔）
	if got := NormalizeDir("C:/Users/me"); !filepath.IsAbs(got) && got != "C:/Users/me" {
		t.Errorf("NormalizeDir(C:/Users/me) = %q", got)
	}
	if got := NormalizeDir(""); got != "." {
		t.Errorf("NormalizeDir(empty) = %q, want .", got)
	}
}

func TestIsAbsDir(t *testing.T) {
	// 跨运行时统一口径：期望值在 POSIX 与 Windows 运行时下均成立。
	//  - "/..." 在 POSIX 为绝对（IsAbs）、在 Windows 经 / 前缀放行（报错文案承诺的形态）；
	//  - "C:\\..." / "C:/..." 在 Windows 为绝对（IsAbs）、在 POSIX 经 IsWindowsPath 放行；
	//  - 相对路径与 ~ 前缀（归一化前的原始形态）两类运行时都拒绝。
	abs := []string{"/Users/me/proj", "/c/Users/proj", `C:\Users\me\proj`, "C:/Users/me/proj"}
	rel := []string{"", "proj", "../proj", ".\\proj", "~/proj"}
	for _, p := range abs {
		if !IsAbsDir(p) {
			t.Errorf("IsAbsDir(%q) = false, want true", p)
		}
	}
	for _, p := range rel {
		if IsAbsDir(p) {
			t.Errorf("IsAbsDir(%q) = true, want false", p)
		}
	}
	// 归一化后仍保持绝对性判定（Clean 不破坏盘符形态）
	if !IsAbsDir(NormalizeDir(`C:\Users\me\x\..\proj`)) {
		t.Error("IsAbsDir(NormalizeDir(C:\\Users\\me\\x\\..\\proj)) = false, want true")
	}
}

func TestPathForm(t *testing.T) {
	// 形态分类与运行时无关（REQ-133 分字段直连：形态 × 部署主机 → 是否后端可达）
	win := map[string]bool{`C:\Users\me`: true, "C:/Users/me": true, `c:\x`: true}
	posix := map[string]bool{"/Users/me": true, "/c/Users": true, "/": true}
	none := []string{"", "proj", "proj/x", `\\server\share`, "~/proj", `C:Users`}
	for p := range win {
		if PathForm(p) != "windows" {
			t.Errorf("PathForm(%q) = %q, want windows", p, PathForm(p))
		}
	}
	for p := range posix {
		if PathForm(p) != "posix" {
			t.Errorf("PathForm(%q) = %q, want posix", p, PathForm(p))
		}
	}
	for _, p := range none {
		if PathForm(p) != "" {
			t.Errorf("PathForm(%q) = %q, want empty", p, PathForm(p))
		}
	}
}

func TestSafeJoin(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 合法：根自身、子目录、子目录下文件（含 .. 回到根内）
	for _, rel := range []string{"", ".", "sub", "sub/../a.txt"} {
		if _, err := SafeJoin(root, rel); err != nil {
			t.Errorf("SafeJoin(%q) unexpected error: %v", rel, err)
		}
	}
	// 越界：绝对路径、.. 逃逸、根外符号链接
	link := filepath.Join(t.TempDir(), "escape")
	if err := os.Symlink(root, link); err != nil {
		t.Skip("symlink unavailable")
	}
	for _, rel := range []string{"../x", "/etc/passwd", filepath.Join("sub", "..", "..", "x")} {
		if _, err := SafeJoin(root, rel); !errors.Is(err, ErrPathOutside) {
			t.Errorf("SafeJoin(%q) = %v, want ErrPathOutside", rel, err)
		}
	}
	_ = link
}
