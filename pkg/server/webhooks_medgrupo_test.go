package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
			assert.Equal(t, c.want, repoHostedOnVcs(c.repo, github))
		})
	}
	assert.False(t, repoHostedOnVcs("https://github.com/org/app", ""), "empty VCS base url never matches")
}
