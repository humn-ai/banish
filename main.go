package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/google/go-github/v53/github"
	version "github.com/hashicorp/go-version"
	"golang.org/x/mod/modfile"
	"golang.org/x/oauth2"
)

type checkRepo struct {
	repo        github.Repository
	treeEntries []*github.TreeEntry
}

type issue struct {
	module      string
	haveVersion *version.Version
	minVersion  *version.Version
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
			out <- *repo
		}

		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}

	close(out)
}

func filterForGoMod(
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

		var treeEntries []*github.TreeEntry
		for _, entry := range tree.Entries {
			path := entry.GetPath()
			if path == "go.mod" || strings.HasSuffix(path, "/go.mod") {
				entry := entry // Capture
				treeEntries = append(treeEntries, entry)
			}
		}

		if len(treeEntries) > 0 {
			out <- checkRepo{repo: repo, treeEntries: treeEntries}
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

func issuesInGoMod(data []byte, banish map[string]*version.Version) ([]issue, error) {
	f, err := modfile.ParseLax("", data, nil)
	if err != nil {
		return []issue{}, err
	}

	var ret []issue
	for _, req := range f.Require {
		if req.Indirect { // TODO: Make option
			continue
		}

		minver, found := banish[req.Mod.Path]
		if !found {
			continue
		}
		if minver == nil {
			ret = append(ret, issue{module: req.Mod.Path})
			continue
		}

		have, err := version.NewVersion(req.Mod.Version)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
			continue
		}

		if have.LessThan(minver) {
			ret = append(ret, issue{module: req.Mod.Path, haveVersion: have, minVersion: minver})
		}
	}
	return ret, nil
}

// check returns false if any issues are found
func check(
	ctx context.Context,
	client *http.Client,
	banish map[string]*version.Version,
	reposToCheck <-chan checkRepo,
) bool {
	var (
		reposWithIssue    int
		totalModuleIssues int
		red               = color.New(color.FgRed)
		green             = color.New(color.FgGreen)
	)

	// TODO: Handle context closing

	for toCheck := range reposToCheck {
		for _, treeEntry := range toCheck.treeEntries {
			mod, err := getTreeFile(client, treeEntry)
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
				green.Printf("PASS %s %s\n", toCheck.repo.GetFullName(), treeEntry.GetPath())
				continue
			}

			reposWithIssue++
			totalModuleIssues += len(issues)
			red.Printf("FAIL %s %s\n", toCheck.repo.GetFullName(), treeEntry.GetPath())
			for _, iss := range issues {
				if iss.minVersion == nil {
					red.Printf("  MOD IMPORTS %s\n", iss.module)
					continue
				}
				red.Printf(
					"  mod imports %s@%s (min version is %s)\n",
					iss.module,
					iss.haveVersion,
					iss.minVersion,
				)
			}
		}
	}

	if reposWithIssue != 0 {
		red.Println()
		red.Printf("== %d repos had %d banished imports ==\n", reposWithIssue, totalModuleIssues)
		return false
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
	banish := make(map[string]*version.Version)
	for _, modEntry := range strings.Split(cfg.rawModules, ",") {
		parts := strings.SplitN(modEntry, "@", 2)
		if len(parts) == 1 {
			banish[parts[0]] = nil
			continue
		}

		v, err := version.NewVersion(parts[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s\n", err)
			os.Exit(3)
		}
		banish[parts[0]] = v
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
	go filterForGoMod(ctx, gclient, cfg.org, cfg.recurse, repos, reposToCheck)
	if !check(ctx, oclient, banish, reposToCheck) {
		os.Exit(2)
	}
}
