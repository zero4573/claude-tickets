// Package assets holds the files ct ships inside its binary.
package assets

import "embed"

// Scaffold is the vault scaffold (vault-scaffold/): what ct vault init
// copies into a vault, plus obsidian-types.json, the property types it
// merges into Obsidian's settings.
//
//go:embed all:vault-scaffold
var Scaffold embed.FS
