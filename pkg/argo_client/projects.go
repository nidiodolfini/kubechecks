package argo_client

import (
	"context"
	"strings"

	"github.com/argoproj/argo-cd/v3/pkg/apiclient/project"
	"github.com/pkg/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/zapier/kubechecks/pkg"
)

// ProjectExists reports whether the AppProject exists in the Argo CD instance
// kubechecks talks to. Apps generated from an ApplicationSet of another
// instance reference projects that only exist there; rendering them can only
// fail with one "appprojects.argoproj.io "x" not found" block per app.
func (a *ArgoClient) ProjectExists(ctx context.Context, name string) (bool, error) {
	closer, projectClient, err := a.client.NewProjectClient()
	if err != nil {
		return false, errors.Wrap(err, "failed to get project client")
	}
	defer pkg.WithErrorLogging(closer.Close, "failed to close connection")

	_, err = projectClient.Get(ctx, &project.ProjectQuery{Name: name})
	if err == nil {
		return true, nil
	}
	if status.Code(err) == codes.NotFound || strings.Contains(err.Error(), "not found") {
		return false, nil
	}
	return false, errors.Wrapf(err, "failed to get project %q", name)
}
