package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zero4573/tix/internal/spec"
	"github.com/zero4573/tix/internal/vaultlock"
)

// tix spec start finishes a pending promote under the vault lock: the file
// another ticket promoted since is held back and listed with what
// resolves it, and promote then refuses it naming that ticket.
func TestStartReplayHoldsBack(t *testing.T) {
	e := newHeldEnv(t)
	e.begin("T-2", nil)
	e.begin("T-1", map[string]string{"a/spec.md": "a1", "b/spec.md": "b1"})
	e.crash("T-1", failPut("b/spec.md"))
	e.put(e.copyFile("T-2", "b/spec.md"), "b-T2")
	e.mustPromote("T-2", "", promoteDone)

	e.clock("T-1")
	res, err := specStart(e.ctx, "T-1", e.r.Slug, nil)
	if err != nil || res.Replay == nil || len(res.Replay.Held) != 1 || res.Replay.Held[0].File != "b/spec.md" {
		t.Fatalf("start: %+v %v", res.Replay, err)
	}
	var out strings.Builder
	printStartReplay(&out, "T-1", res.Replay)
	for _, want := range []string{
		"acme-app: held back b/spec.md: projects/ holds T-2's recorded after copy (projects/acme-app/spec-versions/acme-app-spec-T-2.md)\n",
		"acme-app: promoted 1 files (a); version projects/acme-app/spec-versions/acme-app-spec-T-1.md\n",
		"run tix spec promote T-1 to resolve\n",
	} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	if e.read(e.proj("b/spec.md")) != "b-T2" || e.read(spec.JournalPath(e.v, "T-1", e.r.Slug)) != "<none>" {
		t.Fatal("start overwrote b or left the journal")
	}
	if _, err := os.Lstat(filepath.Join(e.v, ".vault.lock.d")); !os.IsNotExist(err) {
		t.Fatalf("start kept the vault lock: %v", err)
	}
	pr := e.mustPromote("T-1", "", promoteRefused)
	if pr.Replayed != nil || len(pr.Conflict.Others) != 1 || pr.Conflict.Others[0].Ticket != "T-2" {
		t.Fatalf("promote: %+v", pr)
	}
}

// With a pending journal and the vault lock held elsewhere, start fails at
// once, naming the holder, and writes nothing.
func TestStartPendingLockHeld(t *testing.T) {
	e := newHeldEnv(t)
	e.begin("T-1", map[string]string{"b/spec.md": "b1"})
	e.crash("T-1", failPut("b/spec.md"))
	if err := vaultlock.TryAcquire(e.v, "tix save T-9"); err != nil {
		t.Fatal(err)
	}
	before := vaultState(t, e.v)
	_, err := specStart(e.ctx, "T-1", e.r.Slug, []string{"a"})
	if err == nil || !strings.Contains(err.Error(), "the vault lock is held by 'tix save T-9'") || !strings.Contains(err.Error(), "run tix spec promote T-1") {
		t.Fatalf("want a lock refusal: %v", err)
	}
	if !reflect.DeepEqual(vaultState(t, e.v), before) {
		t.Fatal("start wrote while the lock was held")
	}
}

// A journal that appears after start's check is left for promote: start
// refuses and writes nothing. A stage without a journal is left alone.
func TestStartJournalAfterCheck(t *testing.T) {
	e := newHeldEnv(t)
	e.begin("T-1", nil)
	stray := filepath.Join(spec.StageDir(e.v, "T-1", e.r.Slug), "moves", "a", "spec.md")
	e.put(stray, "a1")
	if _, err := specStart(e.ctx, "T-1", e.r.Slug, []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if e.read(stray) != "a1" {
		t.Fatal("start removed a stage without a journal")
	}

	var before map[string]string
	startChecked = func() {
		e.put(spec.JournalPath(e.v, "T-1", e.r.Slug), spec.JournalHeader+"\n")
		before = vaultState(t, e.v)
	}
	defer func() { startChecked = func() {} }()
	_, err := specStart(e.ctx, "T-1", e.r.Slug, []string{"b"})
	if err == nil || !strings.Contains(err.Error(), "promote pending: run tix spec promote T-1") {
		t.Fatalf("want a refusal: %v", err)
	}
	if !reflect.DeepEqual(vaultState(t, e.v), before) {
		t.Fatal("start wrote after the journal appeared")
	}
}
