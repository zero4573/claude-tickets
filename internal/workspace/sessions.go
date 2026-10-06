package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/zero4573/claude-tickets/internal/gitx"
	"github.com/zero4573/claude-tickets/internal/lock"
	"github.com/zero4573/claude-tickets/internal/proc"
)

// SessionsFile records the foreground sessions ct ran in a workspace (ct
// start without a multiplexer, ct kb): which process, on which host, so
// Running can tell a live session from a dead one without a window. A
// list, as a kb workspace can host several sessions at once. Kept apart
// from workspace.json, whose format stays stable.
func SessionsFile(dir string) string { return filepath.Join(dir, ".sessions.json") }

// Record is one session's process: its PID, host and proc.Identity (so a
// reused PID doesn't count).
type Record struct {
	PID     int    `json:"pid"`
	Host    string `json:"host"`
	Proc    string `json:"proc"`
	Command string `json:"command"`
	Started string `json:"started"`
}

type sessionsDoc struct {
	Sessions []Record `json:"sessions"`
}

// Seams for tests
var (
	inContainer = gitx.InContainer
	hostname    = os.Hostname
	identity    = proc.Identity
)

// RecordSession records this process as a session of dir (command: start,
// feedback, kb), dropping the records of dead sessions on this host. Call
// it just before exec: the process that replaces ct keeps its PID and
// start time.
func RecordSession(dir, command string) error {
	pid := os.Getpid()
	host, _ := hostname()
	id, _ := identity(pid) // "" where it can't be told: such a record is never proved alive or dead
	return updateSessions(dir, func(recs []Record) []Record {
		recs = alive(recs)
		return append(recs, Record{PID: pid, Host: host, Proc: id, Command: command, Started: time.Now().Format(time.RFC3339)})
	})
}

// PruneSessions drops the records of sessions that have provably ended on
// this host (a no-op without a .sessions.json).
func PruneSessions(dir string) error {
	if _, err := os.Stat(SessionsFile(dir)); err != nil {
		return nil
	}
	return updateSessions(dir, alive)
}

func alive(recs []Record) []Record {
	if inContainer() {
		return recs // its PIDs aren't the host's: nothing can be proved dead
	}
	var out []Record
	for _, r := range recs {
		if recordLiveness(r) != LivenessDead {
			out = append(out, r)
		}
	}
	return out
}

func updateSessions(dir string, fn func([]Record) []Record) error {
	file := SessionsFile(dir)
	return lock.With(file+".lock", func() error {
		doc := readSessions(file)
		doc.Sessions = fn(doc.Sessions)
		if doc.Sessions == nil {
			doc.Sessions = []Record{}
		}
		data, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			return err
		}
		tmp := file + ".tmp"
		if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, file)
	})
}

func readSessions(file string) sessionsDoc {
	var doc sessionsDoc
	if data, err := os.ReadFile(file); err == nil {
		_ = json.Unmarshal(data, &doc)
	}
	return doc
}

type Liveness int

const (
	LivenessUnknown Liveness = iota // no record, or none that can be checked here
	LivenessAlive
	LivenessDead
)

// sessionLiveness: Alive when any recorded session's process is still the
// one recorded (that record returned), Dead when every record is provably
// gone, Unknown otherwise: no records, records from another host, an OS
// that can't tell, or inside a container (its own PID namespace).
func sessionLiveness(dir string) (Liveness, Record) {
	if inContainer() {
		return LivenessUnknown, Record{}
	}
	recs := readSessions(SessionsFile(dir)).Sessions
	if len(recs) == 0 {
		return LivenessUnknown, Record{}
	}
	dead := 0
	for _, r := range recs {
		switch recordLiveness(r) {
		case LivenessAlive:
			return LivenessAlive, r
		case LivenessDead:
			dead++
		}
	}
	if dead == len(recs) {
		return LivenessDead, Record{}
	}
	return LivenessUnknown, Record{}
}

func recordLiveness(r Record) Liveness {
	if host, err := hostname(); err != nil || r.Host != host || r.Proc == "" {
		return LivenessUnknown
	}
	id, err := identity(r.PID)
	switch {
	case errors.Is(err, proc.ErrNotRunning):
		return LivenessDead
	case err != nil:
		return LivenessUnknown
	case id != r.Proc:
		return LivenessDead // the PID now belongs to another process
	}
	return LivenessAlive
}
