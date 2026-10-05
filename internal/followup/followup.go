// Package followup writes follow-up notes: when an unattended command
// (ct sync, ct graph index, ct ws gc) leaves something for you to check or
// do, it's a task in a note in the vault's inbox/, which the vault's task
// views (pending, Tickets) pick up.
package followup

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Write puts tasks (deduplicated) in
// <vault>/inbox/<yyyy-MM-dd-HHmm>-<cmd>-follow-ups.md, tagged #cmd, with
// links to related notes, and returns the note's path.
// Nothing is written ("" returned) without tasks or in a folder that isn't
// an Obsidian vault.
func Write(vault, cmd, what string, tasks []string, links ...string) (string, error) {
	seen := map[string]bool{}
	var uniq []string
	for _, t := range tasks {
		if t = strings.TrimSpace(t); t != "" && !seen[t] {
			seen[t] = true
			uniq = append(uniq, t)
		}
	}
	if len(uniq) == 0 {
		return "", nil
	}
	if st, err := os.Stat(filepath.Join(vault, ".obsidian")); err != nil || !st.IsDir() {
		return "", nil
	}
	sort.Strings(uniq)
	now := time.Now()
	today := now.Format("2006-01-02")
	name := now.Format("2006-01-02-1504") + "-" + cmd + "-follow-ups"
	var b strings.Builder
	fmt.Fprintf(&b, "created: %s\nupdated: %s\nstatus: active\ntype: follow-ups\nsource-command: %s\ntags: [follow-ups, %s]\n---\n",
		today, today, cmd, cmd)
	fmt.Fprintf(&b, "# %s: follow-ups\n\n%s left these for you (%s). Tick them off here; delete the note\nwhen done. Open tasks also show in [[pending]] and [[tickets.base|Tickets]].\n\n## Tasks\n",
		cmd, what, now.Format("2006-01-02 15:04"))
	for _, t := range uniq {
		fmt.Fprintf(&b, "- [ ] %s #%s ➕ %s\n", t, cmd, today)
	}
	if len(links) > 0 {
		b.WriteString("\n## Related\n")
		for _, l := range links {
			fmt.Fprintf(&b, "- [[%s]]\n", l)
		}
	}
	inbox := filepath.Join(vault, "inbox")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		return "", err
	}
	// A second run in the same minute gets its own note (-2, -3, ...)
	for i := 1; ; i++ {
		n := name
		if i > 1 {
			n = fmt.Sprintf("%s-%d", name, i)
		}
		note := filepath.Join(inbox, n+".md")
		f, err := os.OpenFile(note, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, err = f.WriteString("---\ntitle: " + n + "\n" + b.String())
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		return note, err
	}
}
