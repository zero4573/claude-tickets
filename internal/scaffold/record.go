package scaffold

import (
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

const recordComment = "What ct has shipped into this vault (ct vault init / ct vault update): for each file, the sha256 of the shipped content (LF line endings, one trailing newline) and the ct version that wrote or confirmed it. A file whose content still has this hash counts as unedited and is updated by ct vault update. Don't edit it by hand."

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
// by Obsidian) and a rename, so a reader never sees half of it.
func writeAtomic(dest string, data []byte) error {
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, "."+filepath.Base(dest)+".ct-tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
