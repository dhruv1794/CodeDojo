// SPDX-License-Identifier: MIT

package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopyDirRewritesInternalSymlinkToCopiedTarget(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "target.txt"), []byte("copied"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Mkdir(filepath.Join(src, "links"), 0o755); err != nil {
		t.Fatalf("mkdir links: %v", err)
	}
	if err := os.Symlink("../target.txt", filepath.Join(src, "links", "target-link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := CopyDir(src, dst); err != nil {
		t.Fatalf("CopyDir() error = %v", err)
	}
	linkPath := filepath.Join(dst, "links", "target-link.txt")
	link, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("Readlink() error = %v", err)
	}
	if filepath.IsAbs(link) {
		t.Fatalf("copied symlink target = %q, want relative target", link)
	}
	data, err := os.ReadFile(linkPath)
	if err != nil {
		t.Fatalf("read copied symlink: %v", err)
	}
	if string(data) != "copied" {
		t.Fatalf("copied symlink content = %q, want copied", data)
	}
}

func TestCopyDirRejectsSymlinkOutsideSource(t *testing.T) {
	t.Parallel()

	src := t.TempDir()
	dst := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatalf("write outside target: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(src, "secret-link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	err := CopyDir(src, dst)
	if err == nil {
		t.Fatal("CopyDir() succeeded, want symlink escape error")
	}
	if !strings.Contains(err.Error(), "resolves outside source tree") {
		t.Fatalf("CopyDir() error = %v, want outside source tree", err)
	}
}
