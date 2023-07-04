package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/google/go-github/v53/github"
	"golang.org/x/oauth2"
)

type checkRepo struct {
	repo      github.Repository
	treeEntry *github.TreeEntry
}

func orgRepos(
	ctx context.Context,
	client *github.Client,
	org string,
	out chan<- github.Repository,
) {
	var opt github.RepositoryListByOrgOptions
	for {
		// TODO: Check on context closing

		repos, resp, err := client.Repositories.ListByOrg(ctx, org, &opt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s", err)
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
	in <-chan github.Repository,
	out chan<- checkRepo,
) {
	for repo := range in {
		// TODO: Check on context closing

		tree, _, err := client.Git.GetTree(
			ctx,
			org,
			repo.GetName(),
			repo.GetDefaultBranch(),
			false,
		)
		if err != nil {
			// TODO: Ignore 404 and 409 errors
			fmt.Fprintf(os.Stderr, "%s", err)
			continue
		}

		for _, entry := range tree.Entries {
			if entry.GetPath() == "go.mod" {
				repo, entry := repo, entry // Capture
				out <- checkRepo{repo: repo, treeEntry: entry}
				break
			}
		}
	}

	close(out)
}

func main() {
	cfg := struct {
		org string
	}{}
	flag.StringVar(&cfg.org, "org", "", "limit scan to one organisation")
	flag.Parse()

	ctx := context.Background()
	oclient := oauth2.NewClient(ctx, oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: os.Getenv("GITHUB_ACCESS_TOKEN")},
	))
	gclient := github.NewClient(oclient)

	repos := make(chan github.Repository, 10)
	reposToCheck := make(chan checkRepo, 10)

	go orgRepos(ctx, gclient, cfg.org, repos)
	go filterForGoMod(ctx, gclient, cfg.org, repos, reposToCheck)

	for toCheck := range reposToCheck {
		fmt.Println(toCheck.repo.GetName())
		resp, err := oclient.Get(toCheck.treeEntry.GetURL())
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s", err)
			continue
		}

		var body struct {
			Content []byte `json:"content"`
		}
		err = json.NewDecoder(resp.Body).Decode(&body)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s", err)
			continue
		}
		resp.Body.Close()

		fmt.Println(string(body.Content))
	}
}
