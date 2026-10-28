package assets

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// GraphMerge: the merge script's helpers are tested with stdlib unittest, run
// on a copy of the script and its test so nothing lands in the source tree.
func TestGraphMergeScriptHelpers(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 isn't installed")
	}
	dir := t.TempDir()
	for _, f := range []string{"ticket-merge.py", "ticket_merge_test.py"} {
		b, err := Files.ReadFile("graph/" + f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, f), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(py, "-I", "-m", "unittest", "discover", "-s", dir, "-p", "*_test.py")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("unittest failed: %v\n%s", err, out)
	}
}
