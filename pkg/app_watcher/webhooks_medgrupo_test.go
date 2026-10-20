package app_watcher

import (
	"testing"

	"github.com/argoproj/argo-cd/v3/pkg/apis/application/v1alpha1"
	"github.com/stretchr/testify/assert"
)

func TestGitRepoURLs(t *testing.T) {
	multi := &v1alpha1.Application{Spec: v1alpha1.ApplicationSpec{
		Sources: v1alpha1.ApplicationSources{
			{RepoURL: "733650068546.dkr.ecr.sa-east-1.amazonaws.com/charts", Chart: "medgrupo-prod-app"},
			{RepoURL: "https://github.com/MEDGRUPOGIT/app", Ref: "values"},
		},
	}}
	assert.Equal(t, []string{"https://github.com/MEDGRUPOGIT/app"}, gitRepoURLs(multi))

	single := &v1alpha1.Application{Spec: v1alpha1.ApplicationSpec{
		Source: &v1alpha1.ApplicationSource{RepoURL: "https://gitlab.com/org/app.git"},
	}}
	assert.Equal(t, []string{"https://gitlab.com/org/app.git"}, gitRepoURLs(single))

	assert.Empty(t, gitRepoURLs(&v1alpha1.Application{}))
}

// With ensure-webhooks off (the default) the watcher must not touch the VCS
// client, which is nil in the informer tests of this package.
func TestEnsureWebhooksDisabledIsNoop(t *testing.T) {
	ctrl := &ApplicationWatcher{}
	ctrl.ensureWebhooks(&v1alpha1.Application{Spec: v1alpha1.ApplicationSpec{
		Source: &v1alpha1.ApplicationSource{RepoURL: "https://github.com/org/app"},
	}})
}
