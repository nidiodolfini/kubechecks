package container

import (
	"context"
	"net/url"
	"strings"

	"github.com/pkg/errors"

	"github.com/zapier/kubechecks/pkg/vcs"
)

// HooksPathPrefix is the path, under webhook-url-prefix, where the VCS hook
// routes are served.
const HooksPathPrefix = "/hooks"

// HooksPrefix returns the URL path prefix of the webhook routes:
// "/" + webhook-url-prefix + "/hooks", without a trailing slash.
func (c Container) HooksPrefix() string {
	return hooksPrefix(c.Config.UrlPrefix)
}

func hooksPrefix(urlPrefix string) string {
	p, err := url.JoinPath("/", urlPrefix, HooksPathPrefix)
	if err != nil {
		return HooksPathPrefix
	}
	return strings.TrimSuffix(p, "/")
}

// WebhookURL is the full URL the VCS has to call for this instance:
// webhook-url-base + hooks prefix + "/<vcs>/project".
func (c Container) WebhookURL() (string, error) {
	return webhookURL(c.Config.WebhookUrlBase, c.Config.UrlPrefix, c.VcsClient.GetName())
}

func webhookURL(urlBase, urlPrefix, vcsName string) (string, error) {
	if urlBase == "" {
		return "", errors.New("must define 'webhook-url-base' to create webhooks")
	}
	full, err := url.JoinPath(urlBase, hooksPrefix(urlPrefix), vcsName, "project")
	if err != nil {
		return "", errors.Wrap(err, "failed to create a webhook url")
	}
	return full, nil
}

// RepoHostedOnVcs reports whether repo (a clone URL from the VcsToArgoMap) lives
// on the VCS this instance talks to. The map also holds Helm/OCI chart
// registries and, for ApplicationSets, un-rendered template URLs such as
// "{{ .chart.repoURL }}"; asking the VCS API about those is at best a 404 and,
// for the templated ones, a panic inside the GitHub client's parseRepo that
// takes the whole process down at boot (medgrupo, 2026-10-20).
func RepoHostedOnVcs(repo, vcsBaseUrl string) bool {
	u, err := url.Parse(repo)
	if err != nil || u.Host == "" || strings.Contains(u.Path, "{{") {
		return false
	}
	base, err := url.Parse(vcsBaseUrl)
	if err != nil || base.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, base.Host)
}

// EnsureRepoWebhook registers this instance's webhook on repo when the repo
// lives on the configured VCS and has no hook for our URL yet. It is shared by
// the boot-time sweep (server.ensureWebhooks) and by the Application watcher,
// which calls it for repos that show up while kubechecks is already running.
// Returns true when a hook was created.
func (c Container) EnsureRepoWebhook(ctx context.Context, repo string) (bool, error) {
	if !RepoHostedOnVcs(repo, c.Config.VcsBaseUrl) {
		return false, nil
	}

	fullUrl, err := c.WebhookURL()
	if err != nil {
		return false, err
	}

	wh, err := c.VcsClient.GetHookByUrl(ctx, repo, fullUrl)
	if err != nil && !errors.Is(err, vcs.ErrHookNotFound) {
		return false, errors.Wrapf(err, "failed to get hook for %s", repo)
	}
	if wh != nil {
		return false, nil
	}

	if err := c.VcsClient.CreateHook(ctx, repo, fullUrl, c.Config.WebhookSecret); err != nil {
		return false, errors.Wrapf(err, "failed to create hook for %s", repo)
	}
	return true, nil
}
