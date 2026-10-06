package scaffold

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/zero4573/claude-tickets/internal/config"
)

// RecordName is the record of what ct shipped into a vault, at its root (a
// dot-file, so Obsidian doesn't show it).
const RecordName = ".scaffold.json"

// RecordFormat is the record's format; a record with a higher one is from a
// newer ct: ignored and never rewritten.
const RecordFormat = 1

const recordComment = "What ct has shipped into this vault (ct vault init / ct vault update): for each file, the sha256 of the shipped content (LF line endings, one trailing newline) and the ct version that wrote or confirmed it. A file whose content still has this hash counts as unedited: ct vault update updates it, unless the hash is from a newer ct, which it keeps. Don't edit it by hand."

// Shipped is a record's entry for one file.
type Shipped struct {
	SHA256  string `json:"sha256"`
	Version string `json:"version"`
}

// Record is a vault's .scaffold.json.
type Record struct {
	Comment string             `json:"_comment"`
	Format  int                `json:"format"`
	Files   map[string]Shipped `json:"files"`
}

type recordState int

const (
	recordAbsent recordState = iota
	recordOK
	recordNewer   // from a newer ct: never rewritten
	recordInvalid // rewritten only when files were written anyway
)

// loadRecord reads a vault's record. One it can't use is treated as absent,
// with a warning saying why.
func loadRecord(vault string) (Record, recordState, string) {
	file := filepath.Join(vault, RecordName)
	empty := Record{Files: map[string]Shipped{}}
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return empty, recordAbsent, ""
	}
	if err != nil {
		return empty, recordInvalid, fmt.Sprintf("%s: %v; using the built-in history only", RecordName, err)
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return empty, recordInvalid, fmt.Sprintf("%s isn't valid JSON (%v); using the built-in history only", RecordName, err)
	}
	if rec.Format > RecordFormat {
		return empty, recordNewer, fmt.Sprintf("%s is from a newer ct; using the built-in history only", RecordName)
	}
	if rec.Files == nil {
		rec.Files = map[string]Shipped{}
	}
	return rec, recordOK, ""
}

func saveRecord(vault string, files map[string]Shipped) error {
	return config.WriteJSON(filepath.Join(vault, RecordName), Record{Comment: recordComment, Format: RecordFormat, Files: files})
}

// writeAtomic writes a file through a dot-file next to it (never indexed
// by Obsidian; its name unique to this write, so concurrent runs never
// share one) and a rename, so a reader never sees half of it. A new file
// gets 0644 (less the umask); with keep, the file gets mode as it is (the
// mode of the copy it replaces).
func writeAtomic(dest string, data []byte, mode fs.FileMode, keep bool) error {
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	perm := fs.FileMode(0o644)
	if keep {
		perm = mode.Perm()
	}
	var tmp string
	var f *os.File
	for {
		tmp = filepath.Join(dir, "."+filepath.Base(dest)+"."+randomID()+".ct-tmp")
		var err error
		f, err = os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) {
			return err
		}
	}
	_, err := f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && keep {
		// the umask applied at creation; the replaced copy's mode wins
		err = os.Chmod(tmp, perm)
	}
	if err == nil {
		err = os.Rename(tmp, dest)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

// randomID is 8 random hex characters.
func randomID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
