package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevRecordedAndCleared(t *testing.T) {
	dir := t.TempDir()
	if err := Ensure(dir, "PROJ-12", "/vault"); err != nil {
		t.Fatal(err)
	}
	info, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Dev != nil {
		t.Fatalf("a new workspace is a dev workspace: %+v", info.Dev)
	}
	if data, _ := os.ReadFile(File(dir)); strings.Contains(string(data), `"dev"`) {
		t.Errorf("dev written while off:\n%s", data)
	}
	if err := SetDev(File(dir), &Dev{Slug: "github-acme-tickets"}); err != nil {
		t.Fatal(err)
	}
	// Other writers keep it
	if err := Put(File(dir), Repo{Slug: "github-acme-tickets", Path: filepath.Join(dir, "github-acme-tickets")}); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(dir, "PROJ-12", "/vault"); err != nil {
		t.Fatal(err)
	}
	info, err = Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Dev == nil || info.Dev.Slug != "github-acme-tickets" {
		t.Fatalf("dev = %+v, want github-acme-tickets", info.Dev)
	}
	if len(info.Repos) != 1 || info.ID != "PROJ-12" {
		t.Errorf("the rest of the file changed: %+v", info)
	}
	if err := SetDev(File(dir), nil); err != nil {
		t.Fatal(err)
	}
	if info, _ = Read(dir); info.Dev != nil {
		t.Errorf("dev still set after clearing: %+v", info.Dev)
	}
	if data, _ := os.ReadFile(File(dir)); strings.Contains(string(data), `"dev"`) {
		t.Errorf("dev left in the file:\n%s", data)
	}
}
