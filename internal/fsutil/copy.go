// SPDX-License-Identifier: MIT

package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

func CopyDir(src, dst string) error {
	srcRoot, err := filepath.EvalSymlinks(src)
	if err != nil {
		return fmt.Errorf("resolve source root: %w", err)
	}
	srcRoot, err = filepath.Abs(srcRoot)
	if err != nil {
		return fmt.Errorf("absolute source root: %w", err)
	}
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		if entry.Type()&os.ModeSymlink != 0 {
			linkTarget, err := safeCopiedLinkTarget(srcRoot, dst, path, target)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, target)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func safeCopiedLinkTarget(srcRoot, dstRoot, linkPath, copiedLinkPath string) (string, error) {
	resolved, err := filepath.EvalSymlinks(linkPath)
	if err != nil {
		return "", fmt.Errorf("resolve symlink %q: %w", linkPath, err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("absolute symlink %q: %w", linkPath, err)
	}
	relTarget, err := filepath.Rel(srcRoot, resolved)
	if err != nil {
		return "", fmt.Errorf("relativize symlink %q: %w", linkPath, err)
	}
	if !filepath.IsLocal(relTarget) {
		return "", fmt.Errorf("symlink %q resolves outside source tree", linkPath)
	}
	copiedTarget := filepath.Join(dstRoot, relTarget)
	rewritten, err := filepath.Rel(filepath.Dir(copiedLinkPath), copiedTarget)
	if err != nil {
		return "", fmt.Errorf("rewrite symlink %q: %w", linkPath, err)
	}
	return rewritten, nil
}
