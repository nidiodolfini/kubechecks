package events

import (
	"context"
	"testing"

	"github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	generatorsmocks "github.com/zapier/kubechecks/mocks/generator/mocks"
	"github.com/zapier/kubechecks/pkg/affected_apps"
	"github.com/zapier/kubechecks/pkg/config"
	"github.com/zapier/kubechecks/pkg/container"
	"github.com/zapier/kubechecks/pkg/git"
)

func appSetIn(ns, name string) v1alpha1.ApplicationSet {
	return v1alpha1.ApplicationSet{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
}

func TestDropForeignAppSets(t *testing.T) {
	ce := &CheckEvent{
		logger: zerolog.Nop(),
		ctr:    container.Container{Config: config.ServerConfig{ArgoCDNamespace: "argocd-apps"}},
	}
	kept := ce.dropForeignAppSets([]v1alpha1.ApplicationSet{
		appSetIn("argocd", "spoke-platform-homol"), // hub instance: not ours
		appSetIn("argocd-apps", "medsoft-homol-apps"),
		appSetIn("", "no-namespace"),
	})
	var names []string
	for _, a := range kept {
		names = append(names, a.Name)
	}
	assert.Equal(t, []string{"medsoft-homol-apps", "no-namespace"}, names)
}

// An ApplicationSet of another instance must never reach the generator: the
// mock has no expectations, so any call would fail the test.
func TestGenerateListOfAffectedApps_SkipsForeignAppSet(t *testing.T) {
	gen := new(generatorsmocks.MockAppsGenerator)
	ce := &CheckEvent{
		logger:    zerolog.Nop(),
		ctr:       container.Container{Config: config.ServerConfig{ArgoCDNamespace: "argocd-apps"}},
		generator: gen,
		matcher: MockMatcher("AffectedApps", []interface{}{
			affected_apps.AffectedItems{ApplicationSets: []v1alpha1.ApplicationSet{appSetIn("argocd", "spoke-platform-homol")}},
			nil,
		}),
	}
	require.NoError(t, ce.GenerateListOfAffectedApps(context.Background(), &git.Repo{}, "main", MockInitMatcherFn()))
	assert.Empty(t, ce.affectedItems.ApplicationSets)
	assert.Empty(t, ce.affectedItems.Applications)
	gen.AssertNotCalled(t, "GenerateApplicationSetApps")
}

// Without an Argo client (unit tests) the project filter is a no-op.
func TestDropAppsOfUnknownProjects_NoClientKeepsApps(t *testing.T) {
	ce := &CheckEvent{logger: zerolog.Nop()}
	apps := []v1alpha1.Application{{Spec: v1alpha1.ApplicationSpec{Project: "infra"}}}
	assert.Len(t, ce.dropAppsOfUnknownProjects(context.Background(), apps), 1)
}
