package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/zero4573/claude-tickets/internal/lock"
	"github.com/zero4573/claude-tickets/internal/note"
	"github.com/zero4573/claude-tickets/internal/vault"
)

const (
	relaySep      = " · "
	relayMaxText  = 200
	relayLockWait = 30 * time.Second
	relayMaxDigit = 6  // digits of an id: R-999999 is the last
	relayMaxClose = 40 // lines kept under ## Closed
)

var (
	relayParties = []string{"user", "po", "lead"}
	relayKinds   = []string{"request", "feature", "feedback", "question", "answer", "decision"}
	relayStates  = []string{"open", "planned", "answered", "copied"}
	relayIDLine  = regexp.MustCompile(`(?m)^R-(\d+) · `)
	relayAnyID   = regexp.MustCompile(`(?m)^R-(\d+)\b`)
	relayIDArg   = regexp.MustCompile(`^R-(\d{1,6})$`)
)

func relayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "relay",
		Short: "Add to and update a ticket's relay.md without id collisions",
	}
	cmd.AddCommand(relayAddCmd(), relayStateCmd(), relayCloseCmd())
	return cmd
}

func relayAddCmd() *cobra.Command {
	var from, to, kind, state string
	cmd := &cobra.Command{
		Use:   "add <ID> --from <user|po|lead> --to <user|po|lead> --kind <kind> [--state <state>] <text>",
		Short: "Append a line to the ticket's relay.md under ## Open and print its R-<n>",
		Long: `Appends "R-<n> · <date> · <from>→<to> · <kind> · <text> · <state>" to the
ticket's relay.md, n being 1 + the highest R-<n> in the file, under an
exclusive lock on relay.md.lock (waits up to 30 s), so the PO and the lead
never take the same id. The text is one line of at most 200 characters
without " · " or control characters. Prints R-<n> and nothing else.
Closed summaries ("R-<n> → ...") count for n too, so an id is never reused.
Kinds: request, feature, feedback, question, answer, decision.
States: open (default), planned, answered, copied.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			path, err := relayPath(ctx.Vault, args[0])
			if err != nil {
				return err
			}
			id, err := relayAdd(path, from, to, kind, state, args[1], time.Now(), relayLockWait)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), id)
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "sender: user, po or lead")
	cmd.Flags().StringVar(&to, "to", "", "receiver: user, po or lead")
	cmd.Flags().StringVar(&kind, "kind", "", "request, feature, feedback, question, answer or decision")
	cmd.Flags().StringVar(&state, "state", "open", "open, planned, answered or copied")
	return cmd
}

func relayCloseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "close <ID> R-<n> <summary>",
		Short: "Move a relay entry from ## Open to ## Closed as \"R-<n> → <summary>\"",
		Long: `Removes the entry from ## Open and appends "R-<n> → <summary>" to ## Closed,
under the same lock as ct relay add, keeping at most 40 Closed lines (the
oldest are dropped). The summary follows the text rules of ct relay add.`,
		Args: cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			path, err := relayPath(ctx.Vault, args[0])
			if err != nil {
				return err
			}
			return relayClose(path, args[1], args[2], relayLockWait)
		},
	}
}

func relayStateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "state <ID> R-<n> <open|planned|answered|copied>",
		Short: "Set the state (last field) of a relay line",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := vault.Require()
			if err != nil {
				return err
			}
			path, err := relayPath(ctx.Vault, args[0])
			if err != nil {
				return err
			}
			return relaySetState(path, args[1], args[2], relayLockWait)
		},
	}
}

// relayPath is the ticket's relay.md, which must exist.
func relayPath(vaultDir, id string) (string, error) {
	if note.Find(vaultDir, id) == "" {
		return "", fmt.Errorf("relay: no ticket %s in the vault", id)
	}
	p := filepath.Join(note.TicketDir(vaultDir, id), "relay.md")
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("relay: %s has no relay.md", id)
	}
	return p, nil
}

func oneOf(what, v string, allowed []string) error {
	for _, a := range allowed {
		if v == a {
			return nil
		}
	}
	return fmt.Errorf("relay: %s must be one of %s, not %q", what, strings.Join(allowed, ", "), v)
}

func relayValidateText(text string) error {
	switch {
	case strings.TrimSpace(text) == "":
		return errors.New("relay: the text is empty")
	case strings.ContainsAny(text, "\r\n\u2028\u2029"):
		return errors.New("relay: the text must be a single line")
	case strings.IndexFunc(text, unicode.IsControl) >= 0:
		return errors.New("relay: the text must not contain control characters or tabs")
	case strings.Contains(text, relaySep):
		return fmt.Errorf("relay: the text must not contain %q", relaySep)
	case utf8.RuneCountInString(text) > relayMaxText:
		return fmt.Errorf("relay: the text is %d characters, the limit is %d", utf8.RuneCountInString(text), relayMaxText)
	}
	return nil
}

// relayWriteLocked runs fn on the file's content under the relay.md.lock
// lock and writes back what it returns (when it changed).
func relayWriteLocked(path string, wait time.Duration, fn func(string) (string, error)) error {
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	err := lock.WithContext(ctx, path+".lock", nil, func() error {
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, err := fn(string(b))
		if err != nil || out == string(b) {
			return err
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(out), 0o644); err != nil {
			return err
		}
		if err := os.Rename(tmp, path); err != nil {
			os.Remove(tmp)
			return err
		}
		return nil
	})
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("relay: %s.lock is held by another process; gave up after %s", filepath.Base(path), wait)
	}
	return err
}

// relayAdd appends a line and returns its id (R-<n>, two digits at least, as the file writes them).
func relayAdd(path, from, to, kind, state, text string, now time.Time, wait time.Duration) (string, error) {
	for _, c := range []struct {
		what, v string
		allowed []string
	}{{"--from", from, relayParties}, {"--to", to, relayParties}, {"--kind", kind, relayKinds}, {"--state", state, relayStates}} {
		if err := oneOf(c.what, c.v, c.allowed); err != nil {
			return "", err
		}
	}
	if err := relayValidateText(text); err != nil {
		return "", err
	}
	var id string
	err := relayWriteLocked(path, wait, func(s string) (string, error) {
		next, err := relayNextID(s)
		if err != nil {
			return "", err
		}
		id = fmt.Sprintf("R-%02d", next)
		line := strings.Join([]string{id, now.Format("2006-01-02"), from + "→" + to, kind, text, state}, relaySep)
		return relayInsertOpen(s, line), nil
	})
	return id, err
}

// relayNextID is 1 + the highest R-<n> at the start of any line of the
// file, Closed summaries included.
func relayNextID(s string) (int, error) {
	next := 1
	for _, m := range relayAnyID.FindAllStringSubmatch(s, -1) {
		if len(m[1]) > relayMaxDigit {
			return 0, fmt.Errorf("relay: R-%s has more than %d digits", m[1], relayMaxDigit)
		}
		if n, _ := strconv.Atoi(m[1]); n >= next {
			next = n + 1
		}
	}
	if next > 999999 {
		return 0, errors.New("relay: ids are used up (R-999999)")
	}
	return next, nil
}

// relayInsertOpen puts line at the end of the "## Open" section (the end
// of the file when nothing follows it, or the section is missing).
func relayInsertOpen(s, line string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	end, open := len(lines), false
	for i, l := range lines {
		if open && strings.HasPrefix(l, "## ") {
			end = i
			break
		}
		if strings.TrimRight(l, " \t") == "## Open" {
			open = true
		}
	}
	if end < len(lines) {
		for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
			end--
		}
	}
	out := append(append(append([]string{}, lines[:end]...), line), lines[end:]...)
	return strings.Join(out, "\n") + "\n"
}

func relaySetState(path, id, state string, wait time.Duration) error {
	if err := oneOf("state", state, relayStates); err != nil {
		return err
	}
	want, err := relayParseID(id)
	if err != nil {
		return err
	}
	return relayWriteLocked(path, wait, func(s string) (string, error) {
		lines := strings.Split(s, "\n")
		for i, l := range lines {
			lm := relayIDLine.FindStringSubmatch(l)
			if lm == nil {
				continue
			}
			if n, _ := strconv.Atoi(lm[1]); n != want {
				continue
			}
			k := strings.LastIndex(l, relaySep)
			lines[i] = l[:k+len(relaySep)] + state
			return strings.Join(lines, "\n"), nil
		}
		return "", fmt.Errorf("relay: no %s in %s", id, filepath.Base(path))
	})
}

func relayParseID(id string) (int, error) {
	m := relayIDArg.FindStringSubmatch(id)
	if m == nil {
		return 0, fmt.Errorf("relay: %q is not an id like R-n (at most %d digits)", id, relayMaxDigit)
	}
	n, _ := strconv.Atoi(m[1])
	return n, nil
}

// relayClose moves the Open entry R-<n> to the end of ## Closed as
// "R-<n> → summary", keeping the last relayMaxClose summaries.
func relayClose(path, id, summary string, wait time.Duration) error {
	want, err := relayParseID(id)
	if err != nil {
		return err
	}
	if err := relayValidateText(summary); err != nil {
		return err
	}
	return relayWriteLocked(path, wait, func(s string) (string, error) {
		lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
		open, found := false, -1
		for i, l := range lines {
			if strings.HasPrefix(l, "## ") {
				open = strings.TrimRight(l, " \t") == "## Open"
				continue
			}
			if !open {
				continue
			}
			if lm := relayIDLine.FindStringSubmatch(l); lm != nil {
				if n, _ := strconv.Atoi(lm[1]); n == want {
					found = i
					break
				}
			}
		}
		if found < 0 {
			return "", fmt.Errorf("relay: no %s under ## Open in %s", id, filepath.Base(path))
		}
		lines = append(lines[:found], lines[found+1:]...)
		entry := fmt.Sprintf("R-%02d → %s", want, summary)
		hdr := -1
		for i, l := range lines {
			if strings.TrimRight(l, " \t") == "## Closed" {
				hdr = i
				break
			}
		}
		if hdr < 0 {
			lines = append(lines, "", "## Closed", entry)
			return strings.Join(lines, "\n") + "\n", nil
		}
		end := len(lines)
		for i := hdr + 1; i < len(lines); i++ {
			if strings.HasPrefix(lines[i], "## ") {
				end = i
				break
			}
		}
		last := end
		for last > hdr+1 && strings.TrimSpace(lines[last-1]) == "" {
			last--
		}
		lines = append(lines[:last], append([]string{entry}, lines[last:]...)...)
		// drop the oldest summaries beyond the cap
		var kept []string
		sums := 0
		for i := hdr + 1; i <= last; i++ {
			if strings.HasPrefix(lines[i], "R-") {
				sums++
			}
		}
		drop := sums - relayMaxClose
		for i, l := range lines {
			if i > hdr && i <= last && drop > 0 && strings.HasPrefix(l, "R-") {
				drop--
				continue
			}
			kept = append(kept, l)
		}
		return strings.Join(kept, "\n") + "\n", nil
	})
}
