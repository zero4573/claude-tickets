package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/zero4573/claude-tickets/internal/launcher"
	"github.com/zero4573/claude-tickets/internal/proc"
)

// onHost runs a test as on the host (the sandbox CI may run in counts as a
// container, where no record can be checked).
func onHost(t *testing.T) {
	t.Helper()
	old := inContainer
	inContainer = func() bool { return false }
	t.Cleanup(func() { inContainer = old })
	if _, err := proc.Identity(os.Getpid()); errors.Is(err, proc.ErrUnsupported) {
		t.Skip("no process identity on this OS")
	}
}

// deadPID is the PID of a process that has ended.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func writeRecords(t *testing.T, dir string, recs ...Record) {
	t.Helper()
	data, _ := json.Marshal(sessionsDoc{recs})
	if err := os.WriteFile(SessionsFile(dir), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func self(t *testing.T) Record {
	host, _ := os.Hostname()
	id, err := proc.Identity(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	return Record{PID: os.Getpid(), Host: host, Proc: id, Command: "start"}
}

func writeState(t *testing.T, dir, state string, age time.Duration) {
	t.Helper()
	f := filepath.Join(dir, ".agent-state")
	if err := os.WriteFile(f, []byte(`{"state": "`+state+`", "ts": "2026-10-06T10:00:00-04:00"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(f, when, when); err != nil {
		t.Fatal(err)
	}
}

func TestRecordSession(t *testing.T) {
	onHost(t)
	dir := t.TempDir()
	if err := RecordSession(dir, "start"); err != nil {
		t.Fatal(err)
	}
	live, rec := sessionLiveness(dir)
	if live != LivenessAlive || rec.PID != os.Getpid() || rec.Command != "start" || rec.Started == "" {
		t.Errorf("after RecordSession: %v %+v", live, rec)
	}
	// a second record, and a dead one dropped on the way
	writeRecords(t, dir, self(t), Record{PID: deadPID(t), Host: self(t).Host, Proc: "linux:x:1"})
	if err := RecordSession(dir, "kb"); err != nil {
		t.Fatal(err)
	}
	if recs := readSessions(SessionsFile(dir)).Sessions; len(recs) != 2 || recs[1].Command != "kb" {
		t.Errorf("records: %+v", recs)
	}
}

func TestSessionLiveness(t *testing.T) {
	onHost(t)
	me := self(t)
	dead := Record{PID: deadPID(t), Host: me.Host, Proc: me.Proc}
	reused := me
	reused.Proc = me.Proc + "-other"
	elsewhere := me
	elsewhere.Host = me.Host + "-elsewhere"
	noID := me
	noID.Proc = ""
	for _, tc := range []struct {
		name string
		recs []Record
		want Liveness
	}{
		{"missing file", nil, LivenessUnknown},
		{"alive", []Record{me}, LivenessAlive},
		{"dead pid", []Record{dead}, LivenessDead},
		{"reused pid", []Record{reused}, LivenessDead},
		{"another host", []Record{elsewhere}, LivenessUnknown},
		{"no identity recorded", []Record{noID}, LivenessUnknown},
		{"alive and dead", []Record{dead, me}, LivenessAlive},
		{"dead and another host", []Record{dead, elsewhere}, LivenessUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.recs != nil {
				writeRecords(t, dir, tc.recs...)
			}
			if got, _ := sessionLiveness(dir); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSessionLivenessInContainer(t *testing.T) {
	onHost(t)
	dir := t.TempDir()
	writeRecords(t, dir, Record{PID: deadPID(t), Host: self(t).Host, Proc: self(t).Proc})
	inContainer = func() bool { return true }
	if got, _ := sessionLiveness(dir); got != LivenessUnknown {
		t.Errorf("in a container: %v, want unknown", got)
	}
	if err := PruneSessions(dir); err != nil || len(readSessions(SessionsFile(dir)).Sessions) != 1 {
		t.Errorf("a container prunes nothing: %v", err)
	}
}

func TestPruneSessions(t *testing.T) {
	onHost(t)
	me := self(t)
	elsewhere := me
	elsewhere.Host += "-elsewhere"
	dir := t.TempDir()
	if err := PruneSessions(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(SessionsFile(dir)); err == nil {
		t.Error("PruneSessions created a file")
	}
	writeRecords(t, dir, Record{PID: deadPID(t), Host: me.Host, Proc: me.Proc}, me, elsewhere)
	if err := PruneSessions(dir); err != nil {
		t.Fatal(err)
	}
	recs := readSessions(SessionsFile(dir)).Sessions
	if len(recs) != 2 || recs[0] != me || recs[1] != elsewhere {
		t.Errorf("after prune: %+v", recs)
	}
}

type fakeLauncher struct {
	list    bool
	windows []string
}

func (fakeLauncher) Name() string                          { return "fake" }
func (f fakeLauncher) Caps() launcher.Caps                 { return launcher.Caps{List: f.list} }
func (f fakeLauncher) Windows(string) []string             { return f.windows }
func (fakeLauncher) Open(launcher.Window) error            { return nil }
func (fakeLauncher) SendKeys(string, string, string) error { return nil }
func (fakeLauncher) Attach(string, string) error           { return nil }

func TestSession(t *testing.T) {
	onHost(t)
	me := self(t)
	dead := Record{PID: deadPID(t), Host: me.Host, Proc: me.Proc}
	none := fakeLauncher{}
	for _, tc := range []struct {
		name  string
		l     launcher.Launcher
		recs  []Record
		state string
		age   time.Duration
		want  By
	}{
		{name: "window open", l: fakeLauncher{list: true, windows: []string{"PROJ-1"}}, recs: []Record{dead}, want: ByWindow},
		{name: "windows not listed", l: fakeLauncher{windows: []string{"PROJ-1"}}, state: "exited", want: ByNone},
		{name: "live pid, no state", l: none, recs: []Record{me}, want: ByPID},
		{name: "live pid, old state", l: none, recs: []Record{me}, state: "idle", age: 72 * time.Hour, want: ByPID},
		{name: "dead pid beats a fresh state", l: none, recs: []Record{dead}, state: "working", want: ByNone},
		{name: "no record, fresh state", l: none, state: "working", want: ByAgentState},
		{name: "no record, old state", l: none, state: "working", age: 25 * time.Hour, want: ByNone},
		{name: "no record, exited", l: none, state: "exited", want: ByNone},
		{name: "nil launcher", recs: []Record{me}, want: ByPID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "PROJ-1")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if tc.recs != nil {
				writeRecords(t, dir, tc.recs...)
			}
			if tc.state != "" {
				writeState(t, dir, tc.state, tc.age)
			}
			st := Session(dir, tc.l, "tickets-acme")
			if st.By != tc.want || st.Running != (tc.want != ByNone) {
				t.Errorf("got %+v, want by %v", st, tc.want)
			}
			if tc.want == ByPID && st.PID != os.Getpid() {
				t.Errorf("PID %d, want %d", st.PID, os.Getpid())
			}
			if tc.state != "" && st.State.State != tc.state {
				t.Errorf("State %q, want %q", st.State.State, tc.state)
			}
			if Running(dir, tc.l, "tickets-acme") != st.Running {
				t.Error("Running disagrees with Session")
			}
		})
	}
}

func TestClaimSession(t *testing.T) {
	onHost(t)
	me := self(t)
	dir := t.TempDir()
	// a dead record doesn't hold the workspace (and is dropped)
	writeRecords(t, dir, Record{PID: deadPID(t), Host: me.Host, Proc: me.Proc})
	if err := ClaimSession(dir, "start"); err != nil {
		t.Fatalf("claim over a dead record: %v", err)
	}
	recs := readSessions(SessionsFile(dir)).Sessions
	if len(recs) != 1 || recs[0].PID != os.Getpid() || recs[0].Command != "start" {
		t.Fatalf("records after claim: %+v", recs)
	}
	// a live one does, and the file is left as it was
	var running *RunningError
	if err := ClaimSession(dir, "feedback"); !errors.As(err, &running) || running.Record.PID != os.Getpid() {
		t.Fatalf("claim over a live record: %v", err)
	}
	if recs := readSessions(SessionsFile(dir)).Sessions; len(recs) != 1 || recs[0].Command != "start" {
		t.Errorf("a refused claim changed the records: %+v", recs)
	}
	// another host's record can't be checked: it doesn't block
	elsewhere := me
	elsewhere.Host += "-elsewhere"
	writeRecords(t, dir, elsewhere)
	if err := ClaimSession(dir, "start"); err != nil {
		t.Errorf("claim next to another host's record: %v", err)
	}
}

// Starts racing for one workspace: the check and the record happen under
// one lock, so exactly one gets it (all claim as this process, which is
// alive, so every later claim sees the first one's record).
func TestClaimSessionRace(t *testing.T) {
	onHost(t)
	dir := t.TempDir()
	const n = 8
	errs := make(chan error, n)
	for range n {
		go func() { errs <- ClaimSession(dir, "start") }()
	}
	won := 0
	for range n {
		err := <-errs
		var running *RunningError
		switch {
		case err == nil:
			won++
		case !errors.As(err, &running):
			t.Errorf("unexpected error: %v", err)
		}
	}
	if won != 1 {
		t.Errorf("%d claims succeeded, want 1", won)
	}
	if recs := readSessions(SessionsFile(dir)).Sessions; len(recs) != 1 {
		t.Errorf("records: %+v", recs)
	}
}
