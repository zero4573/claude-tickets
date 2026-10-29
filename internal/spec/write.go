package spec

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// rename is the write seam: tests swap it to fail the second rename.
var rename = os.Rename

func randSuffix() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// WriteTree writes t as the folder root, atomically: the files go into a
// sibling .<name>.tmp-<random> folder that is then renamed into place. An
// existing root is first renamed to .<name>.old-<random>, the new folder
// renamed in, and the old one removed (a rename onto an existing folder
// fails on Windows). A root missing with a leftover .old-* is restored
// first (a crash between the two renames) and leftover .tmp-* folders are
// removed, so a folder that exists is always complete. An empty Tree still
// creates the folder.
func WriteTree(root string, t Tree) error {
	root = filepath.Clean(root)
	parent, name := filepath.Dir(root), filepath.Base(root)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	olds, _ := filepath.Glob(filepath.Join(parent, "."+globEscape(name)+".old-*"))
	if _, err := os.Lstat(root); os.IsNotExist(err) && len(olds) > 0 {
		if err := rename(olds[len(olds)-1], root); err != nil {
			return err
		}
		olds = olds[:len(olds)-1]
	}
	tmps, _ := filepath.Glob(filepath.Join(parent, "."+globEscape(name)+".tmp-*"))
	for _, p := range append(tmps, olds...) {
		_ = os.RemoveAll(p)
	}

	tmp := filepath.Join(parent, "."+name+".tmp-"+randSuffix())
	if err := os.Mkdir(tmp, 0o755); err != nil {
		return err
	}
	fail := func(err error) error {
		_ = os.RemoveAll(tmp)
		return err
	}
	for _, p := range t.paths() {
		dst := filepath.Join(tmp, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fail(err)
		}
		if err := os.WriteFile(dst, t[p], 0o644); err != nil {
			return fail(err)
		}
	}
	if _, err := os.Lstat(root); err != nil {
		if !os.IsNotExist(err) {
			return fail(err)
		}
		if err := rename(tmp, root); err != nil {
			return fail(err)
		}
		return nil
	}
	old := filepath.Join(parent, "."+name+".old-"+randSuffix())
	if err := rename(root, old); err != nil {
		return fail(err)
	}
	if err := rename(tmp, root); err != nil {
		_ = os.RemoveAll(tmp)
		return err // old stays; the next write restores it
	}
	_ = os.RemoveAll(old)
	return nil
}

func globEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, "*", `\*`, "?", `\?`, "[", `\[`).Replace(s)
}
