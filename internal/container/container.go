// Package container runs the graphify image with podman or docker:
// CLAUDE_TICKETS_CONTAINER, else config.json's container, else whichever
// is installed and working.
package container

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zero4573/claude-tickets/internal/config"
)

// Runtime is the container runtime to use: podman or docker. podman's
// docker alias counts as podman, since it takes podman's flags.
func Runtime() (string, error) {
	rt := os.Getenv("CLAUDE_TICKETS_CONTAINER")
	if rt == "" {
		rt = config.Get("container")
	}
	if rt == "" {
		if rt, ok := Detect(); ok {
			return rt, nil
		}
		return "", fmt.Errorf("no container runtime: install podman or docker")
	}
	if rt != "podman" && rt != "docker" {
		return "", fmt.Errorf("container runtime must be podman or docker, not '%s'", rt)
	}
	path, err := exec.LookPath(rt)
	if err != nil {
		return "", fmt.Errorf("%s isn't installed (CLAUDE_TICKETS_CONTAINER / ct vault configure --section runtime)", rt)
	}
	if rt == "docker" {
		if r, err := filepath.EvalSymlinks(path); err == nil && filepath.Base(r) == "podman" {
			return "podman", nil
		}
		if out, _ := exec.Command("docker", "--version").Output(); strings.Contains(strings.ToLower(string(out)), "podman") {
			return "podman", nil
		}
	}
	return rt, nil
}

func Detect() (string, bool) {
	for _, rt := range []string{"podman", "docker"} {
		if _, err := exec.LookPath(rt); err == nil && exec.Command(rt, "info").Run() == nil {
			return rt, true
		}
	}
	return "", false
}

func ImageExists(rt, image string) bool {
	if rt == "podman" {
		return exec.Command("podman", "image", "exists", image).Run() == nil
	}
	return exec.Command("docker", "image", "inspect", image).Run() == nil
}

// RunArgs turns SELinux labels off (the mounts are the user's own files)
// and runs docker as the user (podman is rootless already).
func RunArgs(rt string, args ...string) []string {
	flags := []string{"--rm", "--security-opt", "label=disable"}
	// (no uids on Windows: Getuid is -1 there)
	if rt == "docker" && os.Getuid() >= 0 {
		flags = append(flags, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()))
	}
	var pre []string
	if slice := os.Getenv("CLAUDE_TICKETS_SYSTEMD_SLICE"); slice != "" && runtime.GOOS == "linux" {
		if _, err := exec.LookPath("systemd-run"); err == nil {
			pre = []string{"systemd-run", "--user", "--scope", "--quiet", "--collect", "--slice=" + slice, "--"}
			if rt == "podman" {
				flags = append(flags, "--cgroup-parent="+slice)
			}
		}
	}
	return append(append(append(pre, rt, "run"), flags...), args...)
}

// Path is where a host path appears inside a container: the same path on
// Linux and macOS (podman machine and Docker Desktop share /Users), and the
// drive under /mnt on Windows (C:\x -> /mnt/c/x, as in WSL; untested).
func Path(host string) string { return containerPath(runtime.GOOS, host) }

func containerPath(goos, host string) string {
	if goos != "windows" {
		return host
	}
	p := strings.ReplaceAll(host, `\`, "/")
	if len(p) >= 2 && p[1] == ':' {
		p = "/mnt/" + strings.ToLower(p[:1]) + p[2:]
	}
	return p
}

func Mount(host string, readOnly bool) []string {
	v := host + ":" + Path(host)
	if readOnly {
		v += ":ro"
	}
	return []string{"-v", v}
}
