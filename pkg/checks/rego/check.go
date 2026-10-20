package rego

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/open-policy-agent/conftest/output"
	"github.com/open-policy-agent/conftest/parser"
	"github.com/open-policy-agent/conftest/runner"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"

	"github.com/zapier/kubechecks/pkg"
	"github.com/zapier/kubechecks/pkg/checks"
	"github.com/zapier/kubechecks/pkg/config"
	"github.com/zapier/kubechecks/pkg/msg"
	"github.com/zapier/kubechecks/telemetry"
)

var tracer = otel.Tracer("pkg/checks/rego")

type emojiable interface {
	ToEmoji(state pkg.CommitState) string
}

type Checker struct {
	locations []string
}

var ErrNoLocationsConfigured = errors.New("no policy locations configured")

func NewChecker(cfg config.ServerConfig) (*Checker, error) {
	var c Checker

	c.locations = cfg.PoliciesLocation
	if len(c.locations) == 0 {
		return nil, ErrNoLocationsConfigured
	}

	return &c, nil
}

var ErrResourceMustHaveKind = errors.New("resource does not have kind")
var ErrResourceMustHaveMetadata = errors.New("resource does not have metadata")
var ErrResourceMustHaveName = errors.New("resource does not have name")

func getFilenameFromRawManifest(manifest string) (string, error) {
	resource := make(map[string]interface{})
	if err := yaml.Unmarshal([]byte(manifest), &resource); err != nil {
		return "", errors.Wrap(err, "failed to unmarshal resource")
	}

	kind, okKind := resource["kind"].(string)
	if !okKind {
		return "", ErrResourceMustHaveKind
	}

	metadata, okMetadata := resource["metadata"].(map[string]interface{})
	if !okMetadata {
		return "", ErrResourceMustHaveMetadata
	}

	name, okName := metadata["name"].(string)
	if !okName {
		return "", ErrResourceMustHaveName
	}

	namespace, okNamespace := metadata["namespace"].(string)
	if !okNamespace {
		return fmt.Sprintf("kind=%s,name=%s.yaml", kind, name), nil
	}

	return fmt.Sprintf("namespace=%s,kind=%s,name=%s.yaml", namespace, kind, name), nil
}

func dumpFiles(manifests []string) (string, error) {
	result, err := os.MkdirTemp("", "kubechecks-manifests-")
	if err != nil {
		return "", errors.Wrap(err, "failed to create temp dir")
	}

	log.Debug().
		Caller().
		Int("manifest_count", len(manifests)).
		Msg("dumping manifests")

	for index, manifest := range manifests {
		filename, err := getFilenameFromRawManifest(manifest)
		if err != nil {
			return result, errors.Wrap(err, "failed to get filename from manifest")
		}

		fullPath := filepath.Join(result, filename)
		manifestBytes := []byte(manifest)
		log.Debug().
			Caller().
			Str("path", fullPath).
			Int("index", index).
			Int("size", len(manifestBytes)).
			Msg("dumping manifest")

		if err = os.WriteFile(fullPath, manifestBytes, 0o666); err != nil {
			return result, errors.Wrapf(err, "failed to write %s", filename)
		}
	}

	return result, nil
}

// Check runs the conftest validation against an application in a given repository
// path. It generates a summary string with the results, which can later be posted
// as a GitLab comment. The validation checks resources against Zapier policies and
// provides feedback for warnings or errors as informational messages. Failure to
// pass a policy check currently does not block deploy.
func (c *Checker) Check(ctx context.Context, request checks.Request) (msg.Result, error) {
	_, span := tracer.Start(ctx, "Conftest")
	defer span.End()

	// If there are no manifests, return early with no changes detected
	if len(request.YamlManifests) == 0 {
		log.Debug().
			Caller().
			Str("app", request.App.Name).
			Msg("no manifests to validate, skipping conftest check")
		return msg.Result{
			State:             pkg.StateNone,
			Summary:           "No manifests to validate",
			NoChangesDetected: true,
		}, nil
	}

	manifestsPath, err := dumpFiles(request.YamlManifests)
	if manifestsPath != "" {
		defer pkg.WipeDir(manifestsPath)
	}
	if err != nil {
		return msg.Result{}, errors.Wrap(err, "failed to write manifests to disk")
	}

	log.Debug().
		Caller().
		Strs("policiesPaths", c.locations).
		Str("manifestsPath", manifestsPath).
		Str("app", request.App.Name).
		Msg("running conftest in dir for application")

	r := runner.TestRunner{
		AllNamespaces:      true,
		NoColor:            true,
		Policy:             c.locations,
		Parser:             parser.YAML,
		RegoVersion:        "v1",
		ShowBuiltinErrors:  request.Container.Config.ShowDebugInfo,
		SuppressExceptions: false,
		Trace:              request.Container.Config.ShowDebugInfo,
	}

	results, err := r.Run(ctx, []string{manifestsPath})
	if err != nil {
		telemetry.SetError(span, err, "ConfTest Run")
		return msg.Result{}, err
	}

	var b bytes.Buffer
	if err := formatConftestResults(&b, results, request.Container.VcsClient); err != nil {
		telemetry.SetError(span, err, "formatConftestResults")
		return msg.Result{}, errors.Wrap(err, "failed to format conftest results")
	}
	resultsMessage := b.String()
	resultsMessage = strings.ReplaceAll(resultsMessage, fmt.Sprintf("%s/", manifestsPath), "")

	failures := false
	warnings := false
	for _, r := range results {
		for _, f := range r.Warnings {
			if !f.Passed() {
				warnings = true
			}
		}
		for _, f := range r.Failures {
			if !f.Passed() {
				failures = true
			}
		}
	}

	if strings.TrimSpace(resultsMessage) == "" {
		resultsMessage = "Passed all policy checks."
	}

	var cr msg.Result
	if failures {
		cr.State = pkg.StateFailure
	} else if warnings {
		cr.State = pkg.StateWarning
	} else {
		cr.State = pkg.StateSuccess
	}

	cr.Summary = "<b>Show Conftest Validation result</b>"
	cr.Details = resultsMessage

	return cr, nil
}

// formatConftestResults writes the failed, warned and skipped checks as a
// Markdown table. The box-drawing table tablewriter used to emit is plain text
// inside the <details> block, so GitHub/GitLab wrap it into an unreadable wall of
// lines; a Markdown table renders as a real table. Successes are omitted.
func formatConftestResults(w io.Writer, checkResults []output.CheckResult, vcs emojiable) error {
	var rows []string
	add := func(state, fileName, message string) {
		rows = append(rows, fmt.Sprintf("| %s | %s | %s |", state, resourceLabel(fileName), mdCell(message)))
	}

	for _, checkResult := range checkResults {
		for _, result := range checkResult.Exceptions {
			add(vcs.ToEmoji(pkg.StateError), checkResult.FileName, result.Message)
		}
		for _, result := range checkResult.Warnings {
			add(vcs.ToEmoji(pkg.StateWarning), checkResult.FileName, result.Message)
		}
		for _, result := range checkResult.Skipped {
			add(":arrow_right:", checkResult.FileName, result.Message)
		}
		for _, result := range checkResult.Failures {
			add(vcs.ToEmoji(pkg.StateFailure), checkResult.FileName, result.Message)
		}
	}

	if len(rows) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "| | Resource | Message |\n|---|---|---|\n%s\n", strings.Join(rows, "\n")); err != nil {
		return errors.Wrap(err, "failed to write table")
	}
	return nil
}

// manifestFileRe matches the file names dumpFiles produces:
// "namespace=<ns>,kind=<Kind>,name=<name>.yaml" (namespace optional).
var manifestFileRe = regexp.MustCompile(`^(?:namespace=([^,]*),)?kind=([^,]*),name=(.*)\.yaml$`)

// resourceLabel turns a dumped manifest file name into "Kind `ns/name`";
// anything else is shown verbatim in code font.
func resourceLabel(fileName string) string {
	m := manifestFileRe.FindStringSubmatch(filepath.Base(fileName))
	if m == nil {
		return code(fileName)
	}
	if m[1] == "" {
		return fmt.Sprintf("%s %s", m[2], code(m[3]))
	}
	return fmt.Sprintf("%s %s", m[2], code(m[1]+"/"+m[3]))
}

// mdCell makes a message safe for a Markdown table cell.
func mdCell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "\r\n", "<br>")
	return strings.ReplaceAll(s, "\n", "<br>")
}

func code(s string) string {
	return "`" + s + "`"
}
