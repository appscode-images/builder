package lib

import (
	"context"
	"github.com/google/go-github/v55/github"
	"golang.org/x/oauth2"
	"net/http"
	"os"
	"time"
)

const skew = 10 * time.Second

func NewGitHubClient(ctx context.Context) *github.Client {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: os.Getenv("GITHUB_TOKEN")},
	)
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc)
}

func GitHubRepoExists(ctx context.Context, client *github.Client, owner, repo string) (bool, error) {
	for {
		_, _, err := client.Repositories.Get(ctx, owner, repo)
		switch e := err.(type) {
		case *github.RateLimitError:
			time.Sleep(time.Until(e.Rate.Reset.Time.Add(skew)))
			continue
		case *github.AbuseRateLimitError:
			time.Sleep(e.GetRetryAfter())
			continue
		case *github.ErrorResponse:
			if e.Response.StatusCode == http.StatusNotFound {
				return false, nil
			}
		default:
			if e != nil {
				return false, err
			}
		}
		return true, nil
	}
}
