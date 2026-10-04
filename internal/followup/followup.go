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

// Write puts tasks (deduplicated) in <vault>/inbox/<date>-<cmd>-follow-ups.md,
// tagged #cmd, with links to related notes, and returns the note's path.
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
	fmt.Fprintf(&b, "---\ntitle: %s\ncreated: %s\nupdated: %s\nstatus: active\ntype: follow-ups\nsource-command: %s\ntags: [follow-ups, %s]\n---\n",
		name, today, today, cmd, cmd)
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
	note := filepath.Join(inbox, name+".md")
	return note, os.WriteFile(note, []byte(b.String()), 0o644)
}
