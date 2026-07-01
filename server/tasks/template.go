// server/tasks/template.go — Asynq task definitions for GitHub-sourced template processing.
//
// Task types:
//
//	template:github — download a GitHub release ZIP, parse all IDS structs
//	                  and the full project skeleton, save to the DB.
//
// Result storage:
//
//	Results are written directly to the database via
//	store.UpdateTemplatePkgVersionReady / store.UpdateTemplatePkgVersionError.
//	There is no intermediate Redis key — the HTTP handler polls the parent
//	template_packages row for status changes. This avoids size concerns: the
//	def_json for a large template can be several hundred KB.
//
// Queue and retry policy:
//
//	Template parsing is CPU-bound (go/ast on multiple files) but fast.
//	Uses the dedicated "templates" queue so device jobs do not starve uploads.
//	MaxRetry is 1: transient failures get one retry; persistent failures are
//	recorded in parse_errors. Timeout is 120s for large repositories.
package tasks

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

const (
	// TypeTemplateGitHub is the task type for parsing a GitHub release as a template.
	TypeTemplateGitHub = "template:github"

	// QueueTemplates is the Asynq queue name for template-related tasks.
	QueueTemplates = "templates"
)

// TemplateGitHubPayload carries the data the worker needs to download a GitHub
// release and parse it as a template package version.
type TemplateGitHubPayload struct {
	// VersionID is the primary key of the template_package_versions row.
	// The worker uses it to call store.UpdateTemplatePkgVersionReady or
	// store.UpdateTemplatePkgVersionError after parsing.
	VersionID string `json:"versionId"`

	// PkgID is the parent template_packages primary key.
	// Included for logging and to verify the parent exists.
	PkgID string `json:"pkgId"`

	// GithubURL is the original URL the specialist submitted.
	// Example: "https://github.com/kemper/my-template/releases/tag/v1.2"
	GithubURL string `json:"githubUrl"`

	// Owner, Repo, Tag are extracted from GithubURL by the submit handler.
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Tag   string `json:"tag"`

	// UploaderUserID is used to apply per-user parser complexity limits.
	UploaderUserID string `json:"uploaderUserId"`

	// JobID is the Redis key suffix for publishing the result.
	// The HTTP handler polls "template:job:{JobID}" until it appears.
	JobID string `json:"jobId,omitempty"`

	// Name is the human-readable display name chosen by the specialist in the
	// "New Project" modal. When non-empty the worker persists it via
	// store.UpdateTemplatePkgMeta instead of deriving the name from readme.md.
	// Empty on re-submit (handleSubmitGithub) — existing name is preserved.
	Name string `json:"name,omitempty"`

	// Tags, Visibility, CategoryID, SubcategoryID are set by the create handler
	// and applied to the template_packages row by the worker after parsing.
	Tags          string `json:"tags,omitempty"`
	Visibility    string `json:"visibility,omitempty"`
	CategoryID    string `json:"categoryId,omitempty"`
	SubcategoryID string `json:"subcategoryId,omitempty"`
}

// NewTemplateGitHubTask constructs an Asynq task for GitHub template processing.
func NewTemplateGitHubTask(p TemplateGitHubPayload) (*asynq.Task, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("tasks: marshal template github payload: %w", err)
	}
	return asynq.NewTask(TypeTemplateGitHub, b,
		asynq.Queue(QueueTemplates),
		asynq.MaxRetry(1),
		asynq.Timeout(120*time.Second),
	), nil
}
