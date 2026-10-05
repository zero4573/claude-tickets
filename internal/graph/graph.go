// Package graph is the code graph of ticket and kb sessions: the graphify
// image (built from assets/graph with podman or docker), its unpacked filesystem
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
	"runtime"
	"time"

	"github.com/zero4573/claude-tickets/assets"
	"github.com/zero4573/claude-tickets/internal/config"
	"github.com/zero4573/claude-tickets/internal/container"
	"github.com/zero4573/claude-tickets/internal/fsx"
)

// BaseImage is the graphify image's base, pinned to a tag and the digest
// of its multi-arch index (UPDATES.md says how to bump). Its Python minor
// version must match the lock's ("# python:" in graphify-requirements.txt).
const BaseImage = "docker.io/library/python:3.12.15-slim-trixie@sha256:29113dcae7aad06daa8e95260fa09f27d62be33b9687ea3774f771d601a02256"

// RootFSSupported: the unpacked filesystem (podman run --rootfs) is a
// Linux fallback; on macOS and Windows the runtime runs in a VM that can't
// see a host directory as a root filesystem, so only the image is used.
var RootFSSupported = runtime.GOOS == "linux"

// sources are the files that go into the image (and its hash).
var sources = []string{"Containerfile", "graphify-requirements.txt", "serve.sh", "ticket-merge.py"}

// Src is the image's sources: $CLAUDE_TICKETS_GRAPH_DIR (the Nix package
// sets it), else config.json's graphDir, else the copy embedded in ct,
// unpacked to the cache.
func Src() (string, error) {
	d := os.Getenv("CLAUDE_TICKETS_GRAPH_DIR")
	// (config.json's, from an older install.sh, only while it's still there)
	if c := config.Get("graphDir"); d == "" && c != "" && fsx.IsDir(c) {
		d = c
	}
	if d == "" {
		var err error
		if d, err = assets.Materialize("graph"); err != nil {
			return "", err
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

func Tag(hash string) string { return "localhost/claude-tickets-graphify:" + hash }

func RootFS(hash string) string {
	return filepath.Join(config.CacheDir(), "graphify", "rootfs-"+hash)
}

func unpacked(rootfs string) bool {
	st, err := os.Stat(filepath.Join(rootfs, "opt", "graphify", "serve.sh"))
	return err == nil && st.Mode()&0o111 != 0
}

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

// Build reports progress on stderr.
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
	if !RootFSSupported {
		return tag, nil
	}
	if force || !unpacked(rootfs) {
		fmt.Fprintf(os.Stderr, "ct graph: unpacking its filesystem into %s\n", rootfs)
		if err := unpack(rt, tag, rootfs); err != nil {
			return "", err
		}
	}
	// Touch ours so only other hashes' filesystems age out (30 days)
	now := time.Now()
	_ = os.Chtimes(rootfs, now, now)
	old, _ := filepath.Glob(filepath.Join(cache, "rootfs-*"))
	for _, d := range old {
		if d != rootfs && fsx.OlderThanDays(d, 30) {
			_ = fsx.RemoveAll(d)
		}
	}
	return tag, nil
}

// unpack goes through a temporary dir, so rootfs is either complete or
// absent.
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
		_ = fsx.RemoveAll(tmp)
		return fmt.Errorf("unpacking %s failed", tag)
	}
	cid := string(regexp.MustCompile(`\s+`).ReplaceAll(cidOut, nil))
	defer exec.Command(rt, "rm", "-f", cid).Run()
	export := exec.Command(rt, "export", cid)
	untar := exec.Command("tar", "--no-same-owner", "--no-same-permissions", "-C", tmp, "-xf", "-")
	pipe, err := export.StdoutPipe()
	if err != nil {
		_ = fsx.RemoveAll(tmp)
		return err
	}
	untar.Stdin = pipe
	if err := export.Start(); err != nil {
		_ = fsx.RemoveAll(tmp)
		return fmt.Errorf("unpacking %s failed", tag)
	}
	if err := untar.Start(); err != nil {
		_ = export.Process.Kill()
		_ = export.Wait()
		_ = fsx.RemoveAll(tmp)
		return fmt.Errorf("unpacking %s failed: %w", tag, err)
	}
	// Only tar may hold the read end: if it dies early, export then gets
	// EPIPE instead of blocking forever
	_ = pipe.Close()
	terr := untar.Wait()
	eerr := export.Wait()
	if terr != nil || eerr != nil || !unpacked(tmp) {
		_ = fsx.RemoveAll(tmp)
		return fmt.Errorf("unpacking %s failed", tag)
	}
	_ = fsx.RemoveAll(rootfs)
	return os.Rename(tmp, rootfs)
}

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
	if !RootFSSupported {
		fmt.Fprintf(w, "rootfs:  not used on %s (the container runtime runs in a VM)\n", runtime.GOOS)
	} else if rootfs := RootFS(hash); unpacked(rootfs) {
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
	mounts := append(container.Mount(ws, false), "-e", "PROJECT_ROOT="+container.Path(ws), "-e", "PROJECTS_ROOT="+container.Path(projectsRoot))
	if fsx.IsDir(projectsRoot) {
		mounts = append(mounts, container.Mount(projectsRoot, true)...)
	}
	if rt, err := container.Runtime(); err == nil && container.ImageExists(rt, tag) {
		return container.RunArgs(rt, append(append([]string{"-i"}, mounts...), tag, "/opt/graphify/serve.sh")...), nil
	}
	if rootfs := RootFS(hash); RootFSSupported && unpacked(rootfs) {
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
			_ = fsx.CopyTree(filepath.Join(src, f), filepath.Join(dest, f))
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
			if p := filepath.Join(d, e.Name()); e.IsDir() && snapshotRe.MatchString(e.Name()) && fsx.OlderThanDays(p, 7) {
				_ = fsx.RemoveAll(p)
			}
		}
	}
}
