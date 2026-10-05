// Package fsx is file and path helpers that work the same on every OS:
// paths are compared through filepath (never by "/"), and copying and
// removing are done in Go rather than with cp or rm.
package fsx

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Within reports whether path is dir or inside it (lexically, after
// cleaning; resolve symlinks first where that matters).
func Within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// Inside reports whether path is strictly inside dir.
func Inside(path, dir string) bool {
	return Within(path, dir) && filepath.Clean(path) != filepath.Clean(dir)
}

// Depth is how many path elements a relative path has ("." is 0).
func Depth(rel string) int {
	rel = filepath.Clean(rel)
	if rel == "." || rel == "" {
		return 0
	}
	return strings.Count(rel, string(filepath.Separator)) + 1
}

func IsDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// OlderThanDays is find's -mtime +n: path was modified n+1 or more days ago.
func OlderThanDays(path string, n int) bool {
	st, err := os.Stat(path)
	return err == nil && time.Since(st.ModTime()) >= time.Duration(n+1)*24*time.Hour
}

// CopyFile is cp -p (mode and modification time kept), creating missing
// parent directories.
func CopyFile(src, dest string) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, st.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	_ = os.Chmod(dest, st.Mode().Perm())
	return os.Chtimes(dest, st.ModTime(), st.ModTime())
}

// CopyTree copies symlinks as links.
func CopyTree(src, dest string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dest, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		default:
			return CopyFile(p, target)
		}
	})
}

// RemoveAll is os.RemoveAll that also gets through read-only directories
// (an unpacked image has some).
func RemoveAll(path string) error {
	if os.RemoveAll(path) == nil {
		return nil
	}
	_ = filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() {
			_ = os.Chmod(p, 0o755)
		}
		return nil
	})
	return os.RemoveAll(path)
}
