package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestURLTrie(t *testing.T) {
	testCases := map[string]struct {
		add          []string
		match        string
		expectResult bool
	}{
		"add nothing, find nothing": {
			add:          []string{},
			match:        "foo",
			expectResult: false,
		},
		"add one thing, find it again": {
			add:          []string{"foo"},
			match:        "foo",
			expectResult: true,
		},
		"add some things, find one": {
			add: []string{
				"foo",
				"bar",
				"baz",
			},
			match:        "bar",
			expectResult: true,
		},
		"add some things, but not the thing we look for": {
			add: []string{
				"foo",
				"bar",
				"baz",
			},
			match:        "wat",
			expectResult: false,
		},
		"find in multiple sub-trees": {
			add: []string{
				"foo/aaa/one",
				"bar/bbb/two",
				"baz/ccc/three",
			},
			match:        "bar/bbb/two",
			expectResult: true,
		},
		"find with a partial match": {
			add: []string{
				"foo/aaa/one",
				"bar/bbb/two",
				"baz/ccc/three",
			},
			match:        "bar/bbb",
			expectResult: true,
		},
		"don't get tricked by a partial match in final element": {
			add: []string{
				"foo/aaa/one",
				"bar/bbb/two",
				"baz/ccc/three",
			},
			match:        "bar/bbb/tw",
			expectResult: false,
		},
		"find among lots of elements several levels deep": {
			add: []string{
				"foo/aaa/one",
				"foo/aaa/two",
				"foo/aaa/three",
			},
			match:        "foo/aaa/two",
			expectResult: true,
		},
		"lots of elements several levels deep but no match": {
			add: []string{
				"foo/aaa/one",
				"foo/aaa/two",
				"foo/aaa/three",
			},
			match:        "foo/aaa/four",
			expectResult: false,
		},
	}

	for description, tc := range testCases {
		t.Run(description, func(t *testing.T) {
			trie := newURLTrie()
			for _, add := range tc.add {
				trie.Add(add)
			}
			result := trie.PartialMatch(tc.match)
			assert.Equal(t, tc.expectResult, result)
		})
	}
}

// These are the examples documented against the function
func TestURLTrieExamples(t *testing.T) {
	// foo/bar/baz against tree containing foo/bar/baz => true
	// foo/bar against tree containing foo/bar/baz => true
	// foo/bar/baz against tree containing foo/bar => false
	// foo/ba against tree containing foo/bar => false
	testCases := []struct {
		left         string
		right        string
		expectResult bool
	}{
		{"foo/bar/baz", "foo/bar/baz", true},
		{"foo/bar", "foo/bar/baz", true},
		{"foo/bar/baz", "foo/bar", false},
		{"foo/ba", "foo/bar", false},
	}

	for _, tc := range testCases {
		description := fmt.Sprintf(
			"%s against a tree containing %s => %v",
			tc.left,
			tc.right,
			tc.expectResult,
		)

		t.Run(description, func(t *testing.T) {
			trie := newURLTrie()
			trie.Add(tc.right)
			result := trie.PartialMatch(tc.left)
			assert.Equal(t, tc.expectResult, result)
		})
	}
}
