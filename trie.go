package main

import (
	"strings"
)

type urlTrie struct {
	edges map[string]*urlTrie
}

func newURLTrie() *urlTrie {
	return &urlTrie{edges: make(map[string]*urlTrie)}
}

func (t *urlTrie) Add(url string) {
	t.addParts(strings.Split(url, "/"))
}

func (t *urlTrie) addParts(parts []string) {
	tip := parts[0]
	rest := parts[1:]

	subTrie, found := t.edges[tip]
	if !found {
		tt := newURLTrie()
		t.edges[tip] = tt
		subTrie = tt
	}
	if len(rest) == 0 {
		return
	}
	subTrie.addParts(rest)
}

// Partial Match returns true if the URL is found, even if only partially
// Examples:
// /foo/bar/baz against tree containing /foo/bar/baz => true
// /foo/bar against tree containing /foo/bar/baz => true
// /foo/bar/baz against tree containing /foo/bar => false
// /foo/ba against tree containing /foo/bar => false
func (t *urlTrie) PartialMatch(url string) bool {
	return t.partialMatchParts(strings.Split(url, "/"))
}

func (t *urlTrie) partialMatchParts(parts []string) bool {
	tip := parts[0]
	rest := parts[1:]

	subTrie, found := t.edges[tip]
	if !found {
		return false
	}
	if len(rest) == 0 {
		return true
	}
	return subTrie.partialMatchParts(rest)
}
