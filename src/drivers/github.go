package drivers

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/oauth2"

	"github.com/google/go-github/github"
)

type githubConfig interface {
	GetCodeHostingDriverName() string
	GetMainBranch() string
	GetURLRepositoryName(string) string
	GetURLHostname(string) string
	GetCodeHostingOriginHostname() string
	GetRemoteOriginURL() string
	GetGitHubToken() string
}

type githubCodeHostingDriver struct {
	originURL  string
	hostname   string
	apiToken   string
	client     *github.Client
	owner      string
	repository string
	mainBranch string
}

func tryCreateGitHub(config githubConfig) CodeHostingDriver {
	if config.GetCodeHostingDriverName() != "github" && config.GetCodeHostingOriginHostname() != "github.com" {
		return nil
	}
	originURL := config.GetRemoteOriginURL()
	result := githubCodeHostingDriver{
		originURL:  originURL,
		hostname:   config.GetURLHostname(originURL),
		apiToken:   config.GetGitHubToken(),
		mainBranch: config.GetMainBranch(),
	}
	repositoryParts := strings.SplitN(config.GetURLRepositoryName(originURL), "/", 2)
	if len(repositoryParts) == 2 {
		result.owner = repositoryParts[0]
		result.repository = repositoryParts[1]
	}
	return result
}

func (d *githubCodeHostingDriver) CanMergePullRequest(branch, parentBranch string) (canMerge bool, defaultCommitMessage string, pullRequestNumber int64, err error) {
	if d.apiToken == "" {
		return false, "", 0, nil
	}
	d.connect()
	pullRequests, err := d.getPullRequests(branch, parentBranch)
	if err != nil {
		return false, "", 0, err
	}
	if len(pullRequests) != 1 {
		return false, "", 0, nil
	}
	return true, d.getDefaultCommitMessage(pullRequests[0]), int64(pullRequests[0].GetNumber()), nil
}

func (d *githubCodeHostingDriver) GetNewPullRequestURL(branch string, parentBranch string) string {
	toCompare := branch
	if parentBranch != d.mainBranch {
		toCompare = parentBranch + "..." + branch
	}
	return fmt.Sprintf("%s/compare/%s?expand=1", d.GetRepositoryURL(), url.PathEscape(toCompare))
}

func (d *githubCodeHostingDriver) GetRepositoryURL() string {
	return fmt.Sprintf("https://%s/%s/%s", d.hostname, d.owner, d.repository)
}

func (d *githubCodeHostingDriver) MergePullRequest(options MergePullRequestOptions) (mergeSha string, err error) {
	d.connect()
	err = d.updatePullRequestsAgainst(options)
	if err != nil {
		return "", err
	}
	return d.mergePullRequest(options)
}

func (d *githubCodeHostingDriver) HostingServiceName() string {
	return "GitHub"
}

func (d *githubCodeHostingDriver) SetOriginHostname(originHostname string) {
	d.hostname = originHostname
}

// Helper

func (d *githubCodeHostingDriver) connect() {
	if d.client == nil {
		ts := oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: d.apiToken},
		)
		tc := oauth2.NewClient(context.Background(), ts)
		d.client = github.NewClient(tc)
	}
}

func (d *githubCodeHostingDriver) getDefaultCommitMessage(pullRequest *github.PullRequest) string {
	return fmt.Sprintf("%s (#%d)", *pullRequest.Title, *pullRequest.Number)
}

func (d *githubCodeHostingDriver) getPullRequests(branch, parentBranch string) ([]*github.PullRequest, error) {
	pullRequests, _, err := d.client.PullRequests.List(context.Background(), d.owner, d.repository, &github.PullRequestListOptions{
		Base:  parentBranch,
		Head:  d.owner + ":" + branch,
		State: "open",
	})
	return pullRequests, err
}

func (d *githubCodeHostingDriver) mergePullRequest(options MergePullRequestOptions) (mergeSha string, err error) {
	if options.PullRequestNumber == 0 {
		return "", fmt.Errorf("cannot merge via Github since there is no pull request")
	}
	if options.LogRequests {
		printLog(fmt.Sprintf("GitHub API: Merging PR #%d", options.PullRequestNumber))
	}
	commitMessageParts := strings.SplitN(options.CommitMessage, "\n", 2)
	githubCommitTitle := commitMessageParts[0]
	githubCommitMessage := ""
	if len(commitMessageParts) == 2 {
		githubCommitMessage = commitMessageParts[1]
	}
	result, _, err := d.client.PullRequests.Merge(context.Background(), d.owner, d.repository, int(options.PullRequestNumber), githubCommitMessage, &github.PullRequestOptions{
		MergeMethod: "squash",
		CommitTitle: githubCommitTitle,
	})
	if err != nil {
		return "", err
	}
	return *result.SHA, nil
}

func (d *githubCodeHostingDriver) updatePullRequestsAgainst(options MergePullRequestOptions) error {
	pullRequests, _, err := d.client.PullRequests.List(context.Background(), d.owner, d.repository, &github.PullRequestListOptions{
		Base:  options.Branch,
		State: "open",
	})
	if err != nil {
		return err
	}
	for _, pullRequest := range pullRequests {
		if options.LogRequests {
			printLog(fmt.Sprintf("GitHub API: Updating base branch for PR #%d", *pullRequest.Number))
		}
		_, _, err = d.client.PullRequests.Edit(context.Background(), d.owner, d.repository, *pullRequest.Number, &github.PullRequest{
			Base: &github.PullRequestBranch{
				Ref: &options.ParentBranch,
			},
		})
		if err != nil {
			return err
		}
	}
	return nil
}
