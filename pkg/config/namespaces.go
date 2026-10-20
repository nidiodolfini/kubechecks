package config

import "github.com/argoproj/argo-cd/v3/util/glob"

// AllowsAppNamespace reports whether an Application or ApplicationSet living in
// ns belongs to the Argo CD instance this kubechecks watches: the controller
// namespace or one of additional-apps-namespaces (regexp globs). An empty
// namespace (a manifest without metadata.namespace) is accepted, since Argo CD
// defaults it to the controller namespace on apply.
func (cfg ServerConfig) AllowsAppNamespace(ns string) bool {
	if ns == "" || ns == cfg.ArgoCDNamespace {
		return true
	}
	return glob.MatchStringInList(cfg.AdditionalAppsNamespaces, ns, glob.REGEXP)
}
