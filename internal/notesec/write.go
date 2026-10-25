package notesec

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zero4573/claude-tickets/internal/vaultlock"
)

// Op computes a note's new bytes from its current ones, and says what it
// touches.
type Op func(src []byte) ([]byte, Target, error)

// Apply runs op on the note at vault/rel under the write rules: the rules
// are checked first, then the vault lock is taken (an owner that already
// holds it keeps it), the note is re-read and op re-run if it changed in
// the meantime, and the result is written to a temp file and renamed over
// it. It reports whether the file changed.
func Apply(c Ctx, rel, owner string, op Op) (bool, error) {
	file, rel, err := realPath(c.Vault, rel)
	if err != nil {
		return false, err
	}
	src, err := os.ReadFile(file)
	if err != nil {
		return false, err
	}
	out, t, err := op(src)
	if err != nil {
		return false, err
	}
	if err := checkWrite(c, rel, src, out, t); err != nil {
		return false, err
	}
	if bytes.Equal(out, src) {
		return false, nil
	}
	if owner == "" {
		owner = fmt.Sprintf("ct-note-%d", os.Getpid())
	}
	held := false
	if dir, err := vaultlock.Dir(c.Vault); err == nil {
		held = vaultlock.Holder(dir) == owner
	}
	if err := vaultlock.Acquire(c.Vault, owner); err != nil {
		return false, err
	}
	if !held {
		defer vaultlock.Release(c.Vault, owner)
	}
	if again, err := os.ReadFile(file); err != nil {
		return false, err
	} else if !bytes.Equal(again, src) {
		if out, t, err = op(again); err != nil {
			return false, err
		}
		if err := checkWrite(c, rel, again, out, t); err != nil {
			return false, err
		}
		if bytes.Equal(out, again) {
			return false, nil
		}
	}
	return true, writeAtomic(file, out)
}

func writeAtomic(file string, data []byte) error {
	mode := os.FileMode(0o644)
	if st, err := os.Stat(file); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), "."+filepath.Base(file)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, file); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// realPath resolves symlinks in the note and its parents and returns the
// real file with its vault-relative path, so the rules see where the write
// really lands. A note that resolves outside the vault is refused.
func realPath(vault, rel string) (file, real string, err error) {
	root, err := filepath.EvalSymlinks(vault)
	if err != nil {
		return "", "", err
	}
	if root, err = filepath.Abs(root); err != nil {
		return "", "", err
	}
	f, err := filepath.EvalSymlinks(filepath.Join(vault, filepath.FromSlash(rel)))
	if err != nil {
		return "", "", err
	}
	r, err := filepath.Rel(root, f)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", "", refuse("refusing to write %s: it resolves outside the vault", rel)
	}
	return f, filepath.ToSlash(r), nil
}

// RealRel is the vault-relative path rel resolves to once symlinks are
// followed (the path Apply writes and the search index lists).
func RealRel(vault, rel string) (string, error) {
	_, real, err := realPath(vault, rel)
	return real, err
}
