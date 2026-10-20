package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAllowsAppNamespace(t *testing.T) {
	cfg := ServerConfig{ArgoCDNamespace: "argocd-apps", AdditionalAppsNamespaces: []string{"/team-.*/", "shared"}}
	cases := []struct {
		ns   string
		want bool
	}{
		{"argocd-apps", true},
		{"", true},
		{"team-x", true}, // regex entre barras, como no argo-cd
		{"shared", true}, // nome exato
		{"shared-x", false},
		{"argocd", false},
		{"other", false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, cfg.AllowsAppNamespace(c.ns), "ns=%q", c.ns)
	}
	assert.False(t, ServerConfig{ArgoCDNamespace: "argocd-apps"}.AllowsAppNamespace("argocd"), "no additional namespaces")
}
