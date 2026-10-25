package notesec

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

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
	file := filepath.Join(c.Vault, filepath.FromSlash(rel))
	src, err := os.ReadFile(file)
	if err != nil {
		return false, err
	}
	out, t, err := op(src)
	if err != nil {
		return false, err
	}
	if err := Check(c, rel, t); err != nil {
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
		if err := Check(c, rel, t); err != nil {
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
