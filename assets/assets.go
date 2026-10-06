// Package assets holds the files ct ships inside its binary, so a single
// executable is a complete install:
//
//	vault-scaffold/        what ct vault init copies into a vault, plus
//	                       obsidian-types.json, the property types it merges
//	                       into Obsidian's settings
//	scaffold-history.json  the hash of every version of each scaffold file
//	                       ct has shipped, so ct vault update knows an
//	                       unedited old copy (generated: go generate ./assets)
//	plugin/                the Claude Code plugin (skills, role agents, hooks)
//	graph/                 the graphify image's sources (ct graph build)
//	obsidian/              the Tasks community plugin's pin (tasks-plugin.json)
//	                       and the workflow's settings for it
//	                       (tasks-settings.json), what ct vault init installs,
//	                       shared with nix/obsidian.nix
//
// The scaffold and obsidian/ are read straight from the embedded files; the
// plugin and the graph sources are unpacked to the cache on first use
// (Materialize), unless the environment points at a copy (the Nix package
// does).
package assets

import "embed"

//go:generate go run ../internal/scaffold/genhistory -o scaffold-history.json

//go:embed all:vault-scaffold
var Scaffold embed.FS

//go:embed scaffold-history.json
var ScaffoldHistory []byte

//go:embed all:plugin all:graph
var Files embed.FS

//go:embed obsidian
var Obsidian embed.FS
