package perception

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteFileAtomicPreservesSymlink is the regression guard for
// Codex's [P2] finding: the previous implementation renamed the temp
// file onto the target path, replacing an existing .tma1-context.md
// symlink with a regular file. After the EvalSymlinks fix, atomic
// writes follow the symlink and update the resolved target, leaving
// the symlink intact.
func TestWriteFileAtomicPreservesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.md")
	link := filepath.Join(dir, ".tma1-context.md")
	if err := os.WriteFile(target, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.md", link); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}

	if err := writeFileAtomic(link, []byte("rewritten\n"), 0o644); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf(".tma1-context.md is no longer a symlink (mode=%v) — atomic write replaced it", info.Mode())
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read resolved target: %v", err)
	}
	if string(got) != "rewritten\n" {
		t.Errorf("resolved target content = %q, want %q", got, "rewritten\n")
	}
}

// TestWriteFileAtomicNewFile guards the common case: target doesn't
// exist yet. EvalSymlinks fails (ENOENT) and we should fall through to
// writing a regular file at the original path.
func TestWriteFileAtomicNewFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "fresh.md")
	if err := writeFileAtomic(target, []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != "hello\n" {
		t.Errorf("target content = %q, want %q", got, "hello\n")
	}
}

// TestWriteFileAtomicPreservesExistingMode guards Copilot's review
// finding: os.WriteFile only applied perm on first create, so a user
// who chmod'd .tma1-context.md to a custom mode expected that to
// stick across updates. The previous implementation forced perm on
// every write, silently undoing the user's chmod.
func TestWriteFileAtomicPreservesExistingMode(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "perms.md")
	if err := os.WriteFile(target, []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o600); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	if err := writeFileAtomic(target, []byte("v2\n"), 0o644); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("mode after update = %v, want 0o600 (user chmod preserved)", got)
	}
}
