package main

import (
	"github.com/fatih/color"
	version "github.com/hashicorp/go-version"
)

type writer struct {
	red   *color.Color
	green *color.Color
}

func newWriter() *writer {
	return &writer{
		red:   color.New(color.FgRed),
		green: color.New(color.FgGreen),
	}
}

func (w *writer) Pass(reponame, path string) {
	w.green.Printf("PASS %s %s\n", reponame, path)
}

func (w *writer) Fail(reponame, path string) {
	w.red.Printf("FAIL %s %s\n", reponame, path)
}

func (w *writer) Issue(module string, haveVer, minVer *version.Version) {
	if minVer == nil {
		w.red.Printf("  MOD IMPORTS %s\n", module)
		return
	}
	w.red.Printf("  mod imports %s@%s (min version is %s)\n", module, haveVer, minVer)
}

func (w *writer) Summary(reposWithIssue, totalModuleIssues int) {
	w.red.Println()
	w.red.Printf("== %d repos had %d banished imports ==\n", reposWithIssue, totalModuleIssues)
}
