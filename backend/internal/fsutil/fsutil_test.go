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
