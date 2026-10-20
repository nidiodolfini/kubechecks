package rego

import (
	"bytes"
	"testing"

	"github.com/open-policy-agent/conftest/output"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zapier/kubechecks/pkg"
)

type fakeEmoji struct{}

func (fakeEmoji) ToEmoji(state pkg.CommitState) string { return ":" + state.BareString() + ":" }

func TestFormatConftestResultsMarkdown(t *testing.T) {
	var b bytes.Buffer
	err := formatConftestResults(&b, []output.CheckResult{
		{
			FileName: "/tmp/kubechecks-123/namespace=devops,kind=ConfigMap,name=api.yaml",
			Failures: []output.Result{{Message: "chave 'A|B' parece segredo"}},
			Warnings: []output.Result{{Message: "sem resources"}},
		},
		{FileName: "kind=ClusterRole,name=reader.yaml", Successes: 3},
	}, fakeEmoji{})
	require.NoError(t, err)

	want := "| | Resource | Message |\n|---|---|---|\n" +
		"| :Warning: | ConfigMap `devops/api` | sem resources |\n" +
		"| :Failed: | ConfigMap `devops/api` | chave 'A\\|B' parece segredo |\n"
	assert.Equal(t, want, b.String())
	assert.NotContains(t, b.String(), "\u2502", "no box-drawing characters")

	b.Reset()
	require.NoError(t, formatConftestResults(&b, []output.CheckResult{{FileName: "kind=A,name=b.yaml", Successes: 1}}, fakeEmoji{}))
	assert.Empty(t, b.String(), "only successes: nothing to show")
}

func TestResourceLabel(t *testing.T) {
	assert.Equal(t, "Deployment `medrobot/chat-api`", resourceLabel("namespace=medrobot,kind=Deployment,name=chat-api.yaml"))
	assert.Equal(t, "ClusterRole `reader`", resourceLabel("kind=ClusterRole,name=reader.yaml"))
	assert.Equal(t, "`weird.txt`", resourceLabel("weird.txt"))
}
