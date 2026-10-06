package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Headless runs a headless claude command (claude -p ... --output-format
// stream-json --verbose) with no input, showing its progress as it goes
// (Progress) on stdout and appending it to log. It returns the command's
// error (an *exec.ExitError when it failed).
func Headless(logFile string, argv []string) error { return HeadlessIn("", logFile, argv) }

// HeadlessIn is Headless with dir as the command's working directory ("":
// the current one).
func HeadlessIn(dir, logFile string, argv []string) error {
	logf, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		Progress(pr, io.MultiWriter(os.Stdout, logf))
		// (if Progress stopped early, claude would block on a full pipe)
		_, _ = io.Copy(io.Discard, pr)
		close(done)
	}()
	err = cmd.Wait()
	pw.Close()
	<-done
	return err
}

var mcpPrefix = regexp.MustCompile(`^mcp__[^_]+__`)

// Progress turns claude's stream-json events into readable lines: the
// assistant's text, one line per tool call (tool and main argument), and
// the result. Lines that aren't JSON pass through.
func Progress(r io.Reader, w io.Writer) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		var e struct {
			Type    string `json:"type"`
			IsError bool   `json:"is_error"`
			Result  string `json:"result"`
			Message struct {
				Content []struct {
					Type  string         `json:"type"`
					Text  string         `json:"text"`
					Name  string         `json:"name"`
					Input map[string]any `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &e) != nil {
			fmt.Fprintln(w, line)
			continue
		}
		switch e.Type {
		case "assistant":
			for _, c := range e.Message.Content {
				switch {
				case c.Type == "text" && c.Text != "":
					fmt.Fprintln(w, c.Text)
				case c.Type == "tool_use":
					fmt.Fprintf(w, "  → %s  %s\n", mcpPrefix.ReplaceAllString(c.Name, ""), toolArg(c.Input))
				}
			}
		case "result":
			prefix := ""
			if e.IsError {
				prefix = "ERROR: "
			}
			fmt.Fprintf(w, "\n%s%s\n", prefix, e.Result)
		}
	}
}

// toolArg is the most telling argument of a tool call, shortened.
func toolArg(in map[string]any) string {
	for _, k := range []string{"jql", "issueIdOrKey", "file_path", "path", "pattern", "command", "name", "query", "q"} {
		v, ok := in[k]
		if !ok || v == nil {
			continue
		}
		s, isStr := v.(string)
		if !isStr {
			b, _ := json.Marshal(v)
			return string(b)
		}
		if r := []rune(s); len(r) > 100 {
			s = string(r[:100])
		}
		return strings.ReplaceAll(s, "\n", " ")
	}
	return ""
}
