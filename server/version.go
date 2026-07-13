package main

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionRaw string

// Version is this Base Station build's own Major.Minor.Patch version.
var Version = strings.TrimSpace(versionRaw)
