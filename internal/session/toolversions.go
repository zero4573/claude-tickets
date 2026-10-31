package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/zero4573/tix/internal/config"
	"github.com/zero4573/tix/internal/fsx"
	"github.com/zero4573/tix/internal/vault"
)

const toolVersionsHeader = "# Written by tix; regenerated on every start, so don't edit it."

// WriteToolVersions writes <dir>/.tool-versions for the repos (slug to
// worktree path, may be empty) from the toolVersions setting of vault, so
// asdf's shims resolve in every folder under dir. golang comes from the
// repos' go.mod files, every other tool from the setting; only installed
// versions are written. A tool with no installed version is reported to
// warn and left out; nothing resolved removes a stale file that starts with
// the header. tix owns the root file: an existing one is overwritten (a
// user-written one included) and `tix ws rm` removes it. The write is atomic.
func WriteToolVersions(dir, vault_ string, repos map[string]string, warn func(string)) error {
	if warn == nil {
		warn = func(string) {}
	}
	setting, optedOut, w := vault.ToolVersionsOf(vault_)
	goOut := false
	for _, t := range optedOut {
		goOut = goOut || t == "golang"
	}
	if w != "" {
		warn(w)
	}
	// No asdf at all (no installs dir): nothing to pin and nothing to warn about
	if fi, err := os.Stat(filepath.Join(asdfRoot(), "installs")); err != nil || !fi.IsDir() {
		warn = func(string) {}
	}
	resolved := map[string]string{}

	goReq, goSlugs := goModRequired(repos)
	if goOut {
		goReq = ""
	}
	if goReq != "" && !goOut {
		if _, ok := setting["golang"]; !ok {
			setting["golang"] = "latest" // unused: go.mod decides
		}
	}
	names := make([]string, 0, len(setting))
	for t := range setting {
		names = append(names, t)
	}
	sort.Strings(names)
	fromGoMod := false
	for _, tool := range names {
		want := setting[tool]
		inst := installedVersions(tool)
		var v string
		if tool == "golang" && goReq != "" {
			v = pickGo(inst, goReq)
			if v == "" {
				warn(fmt.Sprintf("tix: .tool-versions: golang %s isn't installed (asdf install golang %s)", goReq, goReq))
			} else {
				fromGoMod = true
			}
		} else {
			v = pickWanted(inst, want)
			if v == "" {
				warn(fmt.Sprintf("tix: .tool-versions: %s %s isn't installed (asdf install %s %s)", tool, want, tool, installHint(want)))
			}
		}
		if v != "" {
			resolved[tool] = v
		}
	}

	file := filepath.Join(dir, ".tool-versions")
	if len(resolved) == 0 {
		if data, err := os.ReadFile(file); err == nil && strings.HasPrefix(string(data), toolVersionsHeader) {
			return os.Remove(file)
		}
		return nil
	}
	tools := make([]string, 0, len(resolved))
	for t := range resolved {
		tools = append(tools, t)
	}
	sort.Strings(tools)
	src := "golang: .workflow.json toolVersions (no go.mod); others: .workflow.json toolVersions."
	if fromGoMod {
		src = "golang: go.mod of " + strings.Join(goSlugs, ", ") + "; others: .workflow.json toolVersions."
	}
	var b strings.Builder
	b.WriteString(toolVersionsHeader + "\n# " + src + "\n")
	for _, t := range tools {
		b.WriteString(t + " " + resolved[t] + "\n")
	}
	return fsx.WriteFileAtomic(file, []byte(b.String()), 0o644)
}

func installHint(want string) string {
	if want == "latest" {
		return "latest"
	}
	return want
}

// asdfRoot is $ASDF_DATA_DIR, else ~/.asdf.
func asdfRoot() string {
	if root := os.Getenv("ASDF_DATA_DIR"); root != "" {
		return root
	}
	return filepath.Join(config.Home(), ".asdf")
}

// installedVersions lists the dir names under the asdf installs of a tool.
func installedVersions(tool string) []string {
	ents, err := os.ReadDir(filepath.Join(asdfRoot(), "installs", tool))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// goModRequired is the highest Go version the repos' go.mod files require
// (toolchain line, else go line) and the slugs of the repos that have one.
func goModRequired(repos map[string]string) (string, []string) {
	req, slugs := "", []string(nil)
	keys := make([]string, 0, len(repos))
	for slug := range repos {
		keys = append(keys, slug)
	}
	sort.Strings(keys)
	for _, slug := range keys {
		data, err := os.ReadFile(filepath.Join(repos[slug], "go.mod"))
		if err != nil {
			continue
		}
		slugs = append(slugs, slug)
		var goLine, tc string
		for _, ln := range strings.Split(string(data), "\n") {
			f := strings.Fields(ln)
			if len(f) < 2 {
				continue
			}
			switch f[0] {
			case "go":
				goLine = f[1]
			case "toolchain":
				tc = strings.TrimPrefix(f[1], "go")
			}
		}
		v := goLine
		if tc != "" {
			v = tc
		}
		if v != "" && (req == "" || compareVersions(v, req) > 0) {
			req = v
		}
	}
	return req, slugs
}

func pickGo(inst []string, req string) string {
	xy := majorMinor(req)
	best := ""
	for _, v := range inst {
		if compareVersions(v, req) >= 0 && majorMinor(v) == xy && (best == "" || compareVersions(v, best) > 0) {
			best = v
		}
	}
	if best != "" {
		return best
	}
	for _, v := range inst {
		if compareVersions(v, req) >= 0 && (best == "" || compareVersions(v, best) > 0) {
			best = v
		}
	}
	return best
}

func pickWanted(inst []string, want string) string {
	best := ""
	for _, v := range inst {
		if want != "latest" && v == want {
			return v
		}
		if best == "" || compareVersions(v, best) > 0 {
			best = v
		}
	}
	if want == "latest" {
		return best
	}
	return ""
}

func majorMinor(v string) string {
	p := strings.Split(v, ".")
	if len(p) > 2 {
		p = p[:2]
	}
	return strings.Join(p, ".")
}

// compareVersions compares per dot-separated part numerically (missing
// parts are 0, non-numeric ones sort lowest).
func compareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		x, y := vpart(pa, i), vpart(pb, i)
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func vpart(p []string, i int) int {
	if i >= len(p) {
		return 0
	}
	n, err := strconv.Atoi(p[i])
	if err != nil {
		return -1
	}
	return n
}
