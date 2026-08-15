// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package watch

import (
	"time"
)

// quarantineThreshold is the number of consecutive failures a child must
// suffer before MultiFactory flips it to Quarantined. Picked so a single
// transient hiccup doesn't generate noise, but a genuinely unreachable
// cluster gets surfaced quickly (3 ticks ≈ 6s at the default refresh rate).
const quarantineThreshold = 3

// defaultQuarantineProbeInterval bounds how often a quarantined child is
// given a chance to recover. Picked so the user perceives a quarantined
// cluster as "gone" rather than "intermittent", but recovery latency stays
// in the same order of magnitude as a typical refresh. Override via
// MultiFactory.SetQuarantineProbeInterval (used by tests).
const defaultQuarantineProbeInterval = 30 * time.Second

// ClusterHealth captures the rollup view the UI flashes / panels expose for
// a single child context.
type ClusterHealth int

const (
	// HealthHealthy: child's last K ticks were either successful or returned
	// only NotFound (discovery divergence, treated as healthy-but-empty).
	HealthHealthy ClusterHealth = iota
	// HealthQuarantined: child has failed quarantineThreshold ticks in a row
	// with non-NotFound errors. The UI flashes a banner and (in a follow-up
	// change) listImpl will skip the child from fan-out until it recovers.
	HealthQuarantined
)

// childHealthState tracks the per-context counters MultiFactory uses to drive
// state transitions. Kept on MultiFactory under healthMx; not exported.
type childHealthState struct {
	state ClusterHealth
	fails int   // consecutive non-NotFound failures
	lastErr error // most recent failure (used in transition flash text)
	// lastProbeAt records when we last attempted a List against this child
	// while quarantined. listImpl skips quarantined children unless this is
	// older than quarantineProbeInterval, giving recovery a chance without
	// paying the per-child timeout on every refresh tick.
	lastProbeAt time.Time
}

// ContextHealthTransition is one Healthy↔Quarantined edge surfaced to the
// view layer. Returned by NewHealthTransitions and consumed by the flash
// dispatch in Browser.TableDataChanged.
type ContextHealthTransition struct {
	Context string
	From    ClusterHealth
	To      ClusterHealth
	Err     error // populated for Healthy → Quarantined transitions
	At      time.Time
}
