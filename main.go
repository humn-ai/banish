package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/google/go-github/v53/github"
	version "github.com/hashicorp/go-version"
	"golang.org/x/mod/modfile"
	"golang.org/x/oauth2"
)

type checkRepo struct {
	repo     github.Repository
	modFiles []*github.TreeEntry
	goFiles  []*github.TreeEntry
}

type blacklisted struct {
	path    string
	version *version.Version
}

type issue struct {
	module      string
	haveVersion *version.Version
	blacklist   blacklisted
}

func orgRepos(
	ctx context.Context,
	client *github.Client,
	org string,
	out chan<- github.Repository,
) {
	var opt github.RepositoryListByOrgOptions
	for {
		// TODO: Handle context closing

		repos, resp, err := client.Repositories.ListByOrg(ctx, org, &opt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
			continue
		}

		for _, repo := range repos {
			if repo.Archived != nil && *repo.Archived {
				continue
			}
			if repo.Disabled != nil && *repo.Disabled {
				continue
			}

			out <- *repo
		}

		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	close(out)
}

func filterForGoFiles(
	ctx context.Context,
	client *github.Client,
	org string,
	recurse bool,
	in <-chan github.Repository,
	out chan<- checkRepo,
) {
	for repo := range in {
		// TODO: Handle context closing

		tree, _, err := client.Git.GetTree(
			ctx,
			org,
			repo.GetName(),
			repo.GetDefaultBranch(),
			recurse,
		)
		if err != nil {
			// TODO: Ignore 409 errors
			fmt.Fprintf(os.Stderr, "%s\n", err)
			continue
		}

		var modFiles []*github.TreeEntry
		var goFiles []*github.TreeEntry
		for _, entry := range tree.Entries {
			path := entry.GetPath()
			if path == "go.mod" || strings.HasSuffix(path, "/go.mod") {
				entry := entry // Capture
				modFiles = append(modFiles, entry)
			} else if strings.HasSuffix(path, ".go") {
				entry := entry // Capture
				goFiles = append(goFiles, entry)
			}
		}

		if len(modFiles) > 0 {
			out <- checkRepo{repo: repo, modFiles: modFiles, goFiles: goFiles}
		}
	}

	close(out)
}

func getTreeFile(client *http.Client, treeEntry *github.TreeEntry) ([]byte, error) {
	resp, err := client.Get(treeEntry.GetURL())
	if err != nil {
		return []byte{}, err
	}

	var body struct {
		Content []byte `json:"content"`
	}
	err = json.NewDecoder(resp.Body).Decode(&body)
	if err != nil {
		return []byte{}, err
	}
	resp.Body.Close()

	return body.Content, nil
}

func issuesInGoMod(data []byte, banish []blacklisted) ([]issue, error) {
	f, err := modfile.ParseLax("", data, nil)
	if err != nil {
		return []issue{}, err
	}

	var ret []issue
	for _, req := range f.Require {
		if req.Indirect { // TODO: Make option
			continue
		}

		for _, bl := range banish {
			if !atLeastPartialURIMatch(req.Mod.Path, bl.path) {
				continue
			}

			if bl.version == nil {
				ret = append(ret, issue{module: req.Mod.Path})
				continue
			}

			have, err := version.NewVersion(req.Mod.Version)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", err)
				continue
			}

			if have.LessThan(bl.version) {
				ret = append(ret, issue{module: req.Mod.Path, haveVersion: have, blacklist: bl})
			}
		}
	}
	return ret, nil
}

// check returns false if any issues are found
func check(
	ctx context.Context,
	client *http.Client,
	banish []blacklisted,
	reposToCheck <-chan checkRepo,
) bool {
	var (
		reposWithIssue    int
		totalModuleIssues int
		write             = newWriter()
	)

	// TODO: Handle context closing

	for toCheck := range reposToCheck {
		for _, modFile := range toCheck.modFiles {
			mod, err := getTreeFile(client, modFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", err)
				continue
			}

			issues, err := issuesInGoMod(mod, banish)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s\n", err)
				continue
			}

			if len(issues) == 0 {
				write.Pass(toCheck.repo.GetFullName(), modFile.GetPath())
				continue
			}

			reposWithIssue++
			totalModuleIssues += len(issues)
			write.Fail(toCheck.repo.GetFullName(), modFile.GetPath())
			for _, iss := range issues {
				write.Issue(iss.module, iss.haveVersion, iss.blacklist.version)
			}
		}
	}

	if reposWithIssue != 0 {
		write.Summary(reposWithIssue, totalModuleIssues)
		return false
	}
	return true
}

func atLeastPartialURIMatch(short, long string) bool {
	// TODO: What a mess! Very inefficient
	if len(short) > len(long) {
		return false
	}

	sParts := strings.Split(short, "/")
	lParts := strings.Split(long, "/")

	if len(sParts) > len(lParts) {
		return false
	}

	for i := 0; i < len(sParts); i++ {
		if sParts[i] != lParts[i] {
			return false
		}
	}

	return true
}

func main() {
	// Quite a lot of config parsing here
	//////

	cfg := struct {
		githubToken string
		org         string
		rawModules  string
		recurse     bool
	}{}
	flag.StringVar(
		&cfg.githubToken,
		"github-token",
		"",
		"token to use for github access (required, alternative to GITHUB_TOKEN env variable)",
	)
	flag.StringVar(&cfg.org, "org", "", "organisation to scan (required)")
	flag.StringVar(
		&cfg.rawModules,
		"modules",
		"",
		"comma-separated list of modules to check for (required)",
	)
	flag.BoolVar(&cfg.recurse, "recurse", true, "search repo trees recursively")
	flag.Parse()

	if flag.NArg() == 0 && flag.NFlag() == 0 {
		flag.Usage()
		os.Exit(0)
	}
	if flag.NArg() != 0 {
		fmt.Fprintf(os.Stderr, "unexpected arguments - %s\n", strings.Join(flag.Args(), " "))
		flag.Usage()
		os.Exit(1)
	}
	if cfg.githubToken == "" {
		cfg.githubToken = os.Getenv("GITHUB_TOKEN")
		if cfg.githubToken == "" {
			fmt.Fprintln(os.Stderr, "-github-token required and not provided")
			flag.Usage()
			os.Exit(1)
		}
	}
	if cfg.org == "" {
		fmt.Fprintln(os.Stderr, "-org required and not provided")
		flag.Usage()
		os.Exit(1)
	}
	if cfg.rawModules == "" {
		fmt.Fprintln(os.Stderr, "-modules required and not provided")
		flag.Usage()
		os.Exit(1)
	}

	// Note: A `nil` entry means no version was supplied - all versions should be banished
	var banish []blacklisted
	for _, modEntry := range strings.Split(cfg.rawModules, ",") {
		parts := strings.SplitN(modEntry, "@", 2)
		if len(parts) == 1 {
			banish = append(banish, blacklisted{path: parts[0]})
			continue
		}

		v, err := version.NewVersion(parts[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
			os.Exit(3)
		}
		banish = append(banish, blacklisted{path: parts[0], version: v})
	}

	// End of config parsing make some clients
	//////

	ctx := context.Background()
	oclient := oauth2.NewClient(ctx, oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: cfg.githubToken},
	))
	gclient := github.NewClient(oclient)

	// Make some channels and start everything running
	//////

	repos := make(chan github.Repository, 10)
	reposToCheck := make(chan checkRepo, 10)
	go orgRepos(ctx, gclient, cfg.org, repos)
	go filterForGoFiles(ctx, gclient, cfg.org, cfg.recurse, repos, reposToCheck)
	if !check(ctx, oclient, banish, reposToCheck) {
		os.Exit(2)
	}
}
