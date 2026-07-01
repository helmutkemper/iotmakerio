// server/tasks/wizard_cleanup.go — Daily cleanup of stale wizard drafts.
// SPDX-FileCopyrightText: 2026 Helmut Kemper
// SPDX-License-Identifier: AGPL-3.0-only
//
// Why this file exists
// ====================
//
// CLAUDE_WIZARD_DESIGN.md §7 mandates that wizard_drafts rows older
// than 30 days without modification be deleted. This is a hygiene
// task, not a security one — abandoned drafts accumulate over time
// and are never read by anyone, so we collect the garbage so the
// table doesn't grow without bound.
//
// The work is wrapped as an Asynq task type — `wizard:cleanup` — so
// the same code path can be invoked three ways:
//
//  1. Daily by the scheduler in cmd/worker/main.go (the normal case).
//  2. Manually by an admin tool that enqueues the task on demand.
//  3. As a unit test that calls the underlying store function
//     directly with a custom maxAge.
//
// The task takes no payload. The retention threshold is hard-coded to
// 30 days and lives in this file rather than the store layer because
// "30 days" is a policy decision that a future schema change shouldn't
// silently override.
//
// Failure handling
// ================
//
// A failed cleanup is non-fatal: we log it at ERROR and let Asynq
// retry per its default policy. Worst case, two consecutive failures
// leave 31-day-old rows in place for a day — no user-visible effect.
// We do NOT panic or escalate; the worker has higher-priority queues
// (devices, templates) that must keep flowing.
package tasks

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hibiken/asynq"

	"server/store"
)

const (
	// TypeWizardCleanup is the Asynq task type emitted daily by the
	// scheduler. The task body is empty — there is no per-invocation
	// configuration.
	TypeWizardCleanup = "wizard:cleanup"

	// QueueCleanup is the Asynq queue name for low-priority hygiene
	// work. Defined here rather than reusing "default" so an admin
	// can give cleanup its own concurrency budget without affecting
	// device or template processing.
	QueueCleanup = "cleanup"

	// wizardDraftMaxAge is the retention window for wizard_drafts rows.
	// Rows untouched for longer are deleted on the next cleanup run.
	// Documented in CLAUDE_WIZARD_DESIGN.md §7.
	wizardDraftMaxAge = 30 * 24 * time.Hour
)

// NewWizardCleanupTask builds a fresh `wizard:cleanup` Asynq task. The
// scheduler in cmd/worker/main.go calls this once per registration;
// admin tools that want to kick off a cleanup out of band call the
// same function.
//
// The task is enqueued onto QueueCleanup with default retry semantics.
func NewWizardCleanupTask() *asynq.Task {
	return asynq.NewTask(
		TypeWizardCleanup,
		nil,
		asynq.Queue(QueueCleanup),
	)
}

// MakeWizardCleanupHandler returns the Asynq handler that performs
// the cleanup. The handler reads no per-task state — every run is
// "delete everything older than 30 days". Returning a HandlerFunc
// (rather than a method on a type) matches the pattern used by every
// other handler in this package.
//
// The handler is paranoid about logging: emitting both the count and
// the elapsed duration on every successful run gives Kemper a clean
// trail to spot anomalies (sudden 10x bumps, or zero counts when the
// schedule slipped).
func MakeWizardCleanupHandler() asynq.HandlerFunc {
	return func(ctx context.Context, t *asynq.Task) error {
		start := time.Now()
		deleted, err := store.CleanupOldWizardDrafts(wizardDraftMaxAge)
		if err != nil {
			log.Printf("[worker/wizard-cleanup] failed: %v", err)
			return fmt.Errorf("cleanup wizard drafts: %w", err)
		}
		log.Printf("[worker/wizard-cleanup] deleted=%d duration=%s",
			deleted, time.Since(start).Round(time.Millisecond))
		return nil
	}
}
