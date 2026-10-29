package vsearch

import "testing"

func TestMergeSpecs(t *testing.T) {
	vault := &Response{Query: "q", Results: []Result{
		{Rank: 1, Path: "a.md", Score: 0.9, Tokens: 10},
		{Rank: 2, Path: "b.md", Score: 0.3, Tokens: 10},
	}}
	specs := []SpecIndexResponse{{Slug: "acme-x", Resp: &Response{Results: []Result{
		{Rank: 1, Path: "specs/c/spec.md", Score: 0.5, Tokens: 10},
	}}}}
	got := MergeSpecs(vault, specs, 10, 0)
	var paths []string
	for _, r := range got.Results {
		paths = append(paths, r.Path)
	}
	want := []string{"a.md", "acme-x:specs/c/spec.md", "b.md"}
	for i := range want {
		if paths[i] != want[i] || got.Results[i].Rank != i+1 {
			t.Fatalf("got %v", paths)
		}
	}
	if r := got.Results[1]; r.Type != "spec" || r.Project != "acme-x" {
		t.Fatalf("%+v", r)
	}
	if len(vault.Results) != 2 || vault.Results[0].Type != "" {
		t.Fatal("vault response modified")
	}
	if got := MergeSpecs(vault, specs, 2, 0); len(got.Results) != 2 {
		t.Fatalf("k not applied: %d", len(got.Results))
	}
	b := MergeSpecs(vault, specs, 10, 15)
	if len(b.Results) != 1 || b.Budget == nil || !b.Budget.Truncated || b.Budget.Used != 10 {
		t.Fatalf("%+v %+v", b.Results, b.Budget)
	}
	if MergeSpecs(vault, nil, 10, 0) != vault {
		t.Fatal("no specs must return the vault response")
	}
}

func TestVaultSpec(t *testing.T) {
	for p, want := range map[string]bool{
		"projects/acme/specs/pay/spec.md": true,
		"projects/acme/specs/pay/x.md":    false,
		"projects/acme/acme.md":           false,
		"other/specs/pay/spec.md":         false,
	} {
		if vaultSpec(p) != want {
			t.Errorf("%s", p)
		}
	}
}
