// server/tasks/device.go — Asynq task definitions for GitHub-sourced device processing.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Task types:
//
//	device:github — download a GitHub release ZIP, parse all IDS structs,
//	                save each one to the blackboxes table.
//
// All task results are published to Redis key "device:job:{jobID}" with a
// 5-minute TTL. The HTTP handler polls that key and returns the result.
//
// Queue: "devices" — separate from "templates" so one type does not starve
// the other. Both are lower priority than "default".
package tasks

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
)

const (
	// TypeDeviceGitHub is the task type for processing a GitHub release as a device.
	TypeDeviceGitHub = "device:github"

	// QueueDevices is the Asynq queue name for device-related tasks.
	QueueDevices = "devices"
)

// DeviceGitHubPayload carries everything the worker needs to download and
// parse a GitHub release as a set of IoTMaker devices.
type DeviceGitHubPayload struct {
	// JobID is the Redis key suffix for publishing the result.
	// The HTTP handler polls "device:job:{JobID}" until it appears.
	JobID string `json:"jobId"`

	// UserID is the authenticated specialist who submitted the URL.
	UserID string `json:"userId"`

	// ExistingIDs maps display_name → existing blackboxes.id for re-parse.
	// Empty map means all structs are new and will get new IDs.
	ExistingIDs map[string]string `json:"existingIds,omitempty"`

	// GithubURL is the original URL the specialist submitted.
	// Example: "https://github.com/kemper/my-device/releases/tag/v1.2"
	GithubURL string `json:"githubUrl"`

	// Owner, Repo, Tag are extracted from GithubURL by the submit handler.
	// The worker uses them to build the GitHub API download URL.
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	Tag   string `json:"tag"`

	// Tags is the comma-separated tag string from the submit request.
	// Applied to every device found in the repository.
	Tags string `json:"tags,omitempty"`

	// Visibility is the initial visibility for all devices found in the release.
	// "public" or "private" (default). Set by the specialist at submit time.
	Visibility string `json:"visibility,omitempty"`

	// DisplayNameHuman is set by the submit handler as owner/repo fallback.
	// The worker overwrites it with the first # heading from readme.md if found.
	DisplayNameHuman string `json:"displayNameHuman,omitempty"`

	// CategoryID and SubcategoryID set the IDE menu placement for all devices
	// found in this repository. Both are optional — empty means uncategorised.
	CategoryID    string `json:"categoryId,omitempty"`
	SubcategoryID string `json:"subcategoryId,omitempty"`
}

// NewDeviceGitHubTask constructs an Asynq task for GitHub device processing.
func NewDeviceGitHubTask(p DeviceGitHubPayload) (*asynq.Task, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("tasks: marshal device github payload: %w", err)
	}
	return asynq.NewTask(TypeDeviceGitHub, b,
		asynq.Queue(QueueDevices),
		asynq.MaxRetry(2),
		asynq.Timeout(120*time.Second),
	), nil
}
