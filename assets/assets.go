// Package assets holds the files ct ships inside its binary, so a single
// executable is a complete install:
//
//	vault-scaffold/  what ct vault init copies into a vault, plus
//	                 obsidian-types.json, the property types it merges into
//	                 Obsidian's settings
//	plugin/          the Claude Code plugin (skills, role agents, hooks)
//	graph/           the graphify image's sources (ct graph build)
//
// The scaffold is read straight from the embedded files; the plugin and the
// graph sources are unpacked to the cache on first use (Materialize),
// unless the environment points at a copy (the Nix package does).
package assets

import "embed"

//go:embed all:vault-scaffold
var Scaffold embed.FS

//go:embed all:plugin all:graph
var Files embed.FS
