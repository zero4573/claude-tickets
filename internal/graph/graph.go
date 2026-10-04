// Package graph is the code graph of ticket and kb sessions: the graphify
// image (built from graph/ with podman or docker), its unpacked filesystem
// for runtimes that start without images, the MCP server merging a
// workspace's graphs, and the main clones' graphs.
package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"

	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/container"
)

// BaseImage is the graphify image's base, pinned to a tag and the digest
// of its multi-arch index (UPDATES.md says how to bump). Its Python minor
// version must match the lock's ("# python:" in graphify-requirements.txt).
const BaseImage = "docker.io/library/python:3.12.15-slim-trixie@sha256:29113dcae7aad06daa8e95260fa09f27d62be33b9687ea3774f771d601a02256"

// sources are the files that go into the image (and its hash).
var sources = []string{"Containerfile", "graphify-requirements.txt", "serve.sh", "ticket-merge.py"}

// Src is the image's sources: $CLAUDE_TICKETS_GRAPH_DIR (set by the
// package), else config.json's graphDir, else ../share/claude-tickets/graph
// next to the ct binary.
func Src() (string, error) {
	d := os.Getenv("CLAUDE_TICKETS_GRAPH_DIR")
	if d == "" {
		d = config.Get("graphDir")
	}
	if d == "" {
		if exe, err := os.Executable(); err == nil {
			if r, err := filepath.EvalSymlinks(exe); err == nil {
				exe = r
			}
			d = filepath.Join(filepath.Dir(exe), "..", "share", "claude-tickets", "graph")
		}
	}
	if _, err := os.Stat(filepath.Join(d, "Containerfile")); err != nil {
		return "", fmt.Errorf("no graphify image sources at %s (set CLAUDE_TICKETS_GRAPH_DIR, or reinstall)", d)
	}
	return d, nil
}

// Hash is the first 12 hex digits of the sha256 of everything that goes
// into the image: the base image, then each source file.
func Hash(src string) (string, error) {
	h := sha256.New()
	io.WriteString(h, BaseImage+"\n")
	for _, f := range sources {
		data, err := os.ReadFile(filepath.Join(src, f))
		if err != nil {
			return "", err
		}
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

// Tag is the image built for a hash.
func Tag(hash string) string { return "localhost/claude-tickets-graphify:" + hash }

// RootFS is where the image of a hash is unpacked.
func RootFS(hash string) string {
	return filepath.Join(config.CacheDir(), "graphify", "rootfs-"+hash)
}

func unpacked(rootfs string) bool {
	st, err := os.Stat(filepath.Join(rootfs, "opt", "graphify", "serve.sh"))
	return err == nil && st.Mode()&0o111 != 0
}

// current is the hash and tag of the image the sources describe.
func current() (hash, tag string, err error) {
	src, err := Src()
	if err != nil {
		return "", "", err
	}
	if hash, err = Hash(src); err != nil {
		return "", "", err
	}
	return hash, Tag(hash), nil
}

// Build builds the image if the runtime doesn't have it (or with force),
// unpacks it into RootFS if it isn't, and returns its tag. Progress goes
// to stderr.
func Build(force bool) (string, error) {
	src, err := Src()
	if err != nil {
		return "", err
	}
	hash, err := Hash(src)
	if err != nil {
		return "", err
	}
	tag := Tag(hash)
	rt, err := container.Runtime()
	if err != nil {
		return "", err
	}
	if force || !container.ImageExists(rt, tag) {
		fmt.Fprintf(os.Stderr, "ct graph: building %s with %s (installs graphify; takes a minute)\n", tag, rt)
		args := []string{"build"}
		// (docker build has no --pull=missing: it pulls a missing base by default)
		if rt == "podman" {
			args = append(args, "--pull=missing")
		}
		args = append(args, "--build-arg", "PYTHON_IMAGE="+BaseImage, "-t", tag, "-f", filepath.Join(src, "Containerfile"), src)
		cmd := exec.Command(rt, args...)
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("building %s failed", tag)
		}
	}
	rootfs := RootFS(hash)
	cache := filepath.Dir(rootfs)
	if force || !unpacked(rootfs) {
		fmt.Fprintf(os.Stderr, "ct graph: unpacking its filesystem into %s\n", rootfs)
		if err := unpack(rt, tag, rootfs); err != nil {
			return "", err
		}
	}
	// Other hashes' unpacked filesystems, untouched for 30 days
	now := time.Now()
	_ = os.Chtimes(rootfs, now, now)
	old, _ := filepath.Glob(filepath.Join(cache, "rootfs-*"))
	for _, d := range old {
		if d != rootfs && olderThanDays(d, 30) {
			removeAll(d)
		}
	}
	return tag, nil
}

// unpack exports a container of tag into rootfs (through a temporary dir,
// so rootfs is either complete or absent).
func unpack(rt, tag, rootfs string) error {
	cache := filepath.Dir(rootfs)
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(cache, ".rootfs.")
	if err != nil {
		return err
	}
	cidOut, err := exec.Command(rt, "create", tag, "/bin/true").Output()
	if err != nil {
		removeAll(tmp)
		return fmt.Errorf("unpacking %s failed", tag)
	}
	cid := string(regexp.MustCompile(`\s+`).ReplaceAll(cidOut, nil))
	defer exec.Command(rt, "rm", "-f", cid).Run()
	export := exec.Command(rt, "export", cid)
	untar := exec.Command("tar", "--no-same-owner", "--no-same-permissions", "-C", tmp, "-xf", "-")
	pipe, err := export.StdoutPipe()
	if err != nil {
		removeAll(tmp)
		return err
	}
	untar.Stdin = pipe
	if err := export.Start(); err != nil {
		removeAll(tmp)
		return fmt.Errorf("unpacking %s failed", tag)
	}
	terr := untar.Run()
	eerr := export.Wait()
	if terr != nil || eerr != nil || !unpacked(tmp) {
		removeAll(tmp)
		return fmt.Errorf("unpacking %s failed", tag)
	}
	removeAll(rootfs)
	return os.Rename(tmp, rootfs)
}

// Status describes what's built, for which hash.
func Status(w io.Writer) error {
	hash, tag, err := current()
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "image:   %s\n", tag)
	if rt, err := container.Runtime(); err == nil {
		if container.ImageExists(rt, tag) {
			fmt.Fprintf(w, "         built (%s)\n", rt)
		} else {
			fmt.Fprintf(w, "         not built (%s): ct graph build\n", rt)
		}
	} else {
		fmt.Fprintln(w, "         no container runtime")
	}
	if rootfs := RootFS(hash); unpacked(rootfs) {
		fmt.Fprintf(w, "rootfs:  %s\n", rootfs)
	} else {
		fmt.Fprintln(w, "rootfs:  not unpacked: ct graph build")
	}
	return nil
}

// MCPCommand is the command line of the MCP server merging workspace ws's
// graphs with the main clones' under projectsRoot: the image through the
// runtime when it has it, else the unpacked filesystem with
// `podman run --rootfs` (which works where the runtime starts with no
// images, e.g. podman inside a container).
func MCPCommand(ws, projectsRoot string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(ws, "workspace.json")); ws == "" || err != nil {
		return nil, errors.New("mcp: <workspace> must be a ticket or kb workspace (with workspace.json)")
	}
	ws, err := filepath.Abs(ws)
	if err != nil {
		return nil, err
	}
	if r, err := filepath.EvalSymlinks(ws); err == nil {
		ws = r
	}
	hash, tag, err := current()
	if err != nil {
		return nil, err
	}
	mounts := []string{"-v", ws + ":" + ws, "-e", "PROJECT_ROOT=" + ws, "-e", "PROJECTS_ROOT=" + projectsRoot}
	if st, err := os.Stat(projectsRoot); err == nil && st.IsDir() {
		mounts = append(mounts, "-v", projectsRoot+":"+projectsRoot+":ro")
	}
	if rt, err := container.Runtime(); err == nil && container.ImageExists(rt, tag) {
		return container.RunArgs(rt, append(append([]string{"-i"}, mounts...), tag, "/opt/graphify/serve.sh")...), nil
	}
	if rootfs := RootFS(hash); unpacked(rootfs) {
		if _, err := exec.LookPath("podman"); err == nil {
			// --rootfs takes the path where the image would go: options before it
			args := append([]string{"podman", "run", "--rm", "-i", "--security-opt", "label=disable"}, mounts...)
			return append(args, "--rootfs", rootfs+":O", "/opt/graphify/serve.sh"), nil
		}
	}
	return nil, errors.New("mcp: the graphify image isn't built (run ct graph build on the host)")
}

// Seed copies a main clone's graph (graph, manifest, cache) into a new
// checkout of it, so the checkout's graph builds incrementally.
func Seed(main, checkout string) {
	src, dest := filepath.Join(main, "graphify-out"), filepath.Join(checkout, "graphify-out")
	if st, err := os.Stat(src); err != nil || !st.IsDir() {
		return
	}
	if _, err := os.Lstat(dest); err == nil {
		return
	}
	if os.MkdirAll(dest, 0o755) != nil {
		return
	}
	for _, f := range []string{"graph.json", "manifest.json", "cache"} {
		if _, err := os.Stat(filepath.Join(src, f)); err == nil {
			_ = exec.Command("cp", "-r", filepath.Join(src, f), dest+"/").Run()
		}
	}
}

var snapshotRe = regexp.MustCompile(`^20[0-9]{2}-[0-9]{2}-[0-9]{2}`)

// PruneSnapshots removes graphify's dated snapshot folders (one per
// rebuild day) older than a week from graphify-out dirs.
func PruneSnapshots(dirs ...string) {
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if p := filepath.Join(d, e.Name()); e.IsDir() && snapshotRe.MatchString(e.Name()) && olderThanDays(p, 7) {
				removeAll(p)
			}
		}
	}
}

// olderThanDays is find's -mtime +n: modified n+1 or more days ago.
func olderThanDays(path string, n int) bool {
	st, err := os.Stat(path)
	return err == nil && time.Since(st.ModTime()) >= time.Duration(n+1)*24*time.Hour
}

// removeAll is os.RemoveAll that also gets through read-only directories
// (an unpacked image has some).
func removeAll(path string) {
	if os.RemoveAll(path) == nil {
		return
	}
	_ = filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
		if err == nil && info.IsDir() {
			_ = os.Chmod(p, 0o755)
		}
		return nil
	})
	_ = os.RemoveAll(path)
}
