package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepoHostedOnVcs(t *testing.T) {
	const github = "https://github.com"
	cases := []struct {
		name string
		repo string
		want bool
	}{
		{"github repo with username", "https://kubechecks@github.com/org/app", true},
		{"github repo, host case-insensitive", "https://GitHub.com/org/app.git", true},
		{"appset template placeholder (panics in parseRepo)", "https://kubechecks@/{{ .chart.repoURL }}", false},
		{"oci chart registry without scheme host", "https://kubechecks@/123.dkr.ecr.sa-east-1.amazonaws.com/charts", false},
		{"other git host", "https://gitlab.com/org/app", false},
		{"garbage", "://nope", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, RepoHostedOnVcs(c.repo, github))
		})
	}
	assert.False(t, RepoHostedOnVcs("https://github.com/org/app", ""), "empty VCS base url never matches")
}

func TestWebhookURL(t *testing.T) {
	cases := []struct {
		name, base, prefix, want string
	}{
		{"no prefix", "https://kubechecks.example.com", "", "https://kubechecks.example.com/hooks/github/project"},
		{"prefix with slashes", "https://kubechecks.example.com/", "/kc/", "https://kubechecks.example.com/kc/hooks/github/project"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := webhookURL(c.base, c.prefix, "github")
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}

	_, err := webhookURL("", "", "github")
	assert.Error(t, err, "empty webhook-url-base is an error")
}
