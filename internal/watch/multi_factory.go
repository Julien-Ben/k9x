// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package watch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/derailed/k9s/internal"
	"github.com/derailed/k9s/internal/client"
	"github.com/derailed/k9s/internal/render"
	"github.com/derailed/k9s/internal/slogs"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
)

// defaultChildListTimeout caps how long a single child's List is allowed to
// block a fan-out tick. Picked so an unreachable cluster doesn't stall the
// whole UI refresh, but long enough that a slow-but-healthy cluster still
// returns under normal conditions.
const defaultChildListTimeout = 3 * time.Second

// ContextError pairs a per-child error with the context name it came from so
// the view layer can surface partial-failure information (banner, panel
// status, …) without losing the cluster-of-origin.
type ContextError struct {
	Context string
	Err     error
}

// childFactory captures the subset of *Factory that MultiFactory consumes
// from each per-context child. Defining it as an interface (instead of using
// *Factory directly) keeps MultiFactory testable with lightweight fakes,
// without introducing an import cycle on dao.LifecycleFactory.
type childFactory interface {
	Client() client.Connection
	Get(gvr *client.GVR, path string, wait bool, sel labels.Selector) (runtime.Object, error)
	List(gvr *client.GVR, ns string, wait bool, sel labels.Selector) ([]runtime.Object, error)
	ForResource(ns string, gvr *client.GVR) (informers.GenericInformer, error)
	CanForResource(ns string, gvr *client.GVR, verbs []string) (informers.GenericInformer, error)
	WaitForCacheSync()
	HasSynced(gvr *client.GVR, ns string) (bool, error)
	Start(ns string)
	Terminate()
	SetActiveNS(ns string) error
	AddForwarder(pf Forwarder)
	ForwarderFor(path string) (Forwarder, bool)
	DeleteForwarder(path string)
	Forwarders() Forwarders
	ValidatePortForwards()
}

// MultiFactory aggregates Factories from multiple kubeconfig contexts behind
// the dao.LifecycleFactory interface. List fans out across all children and
// merges results, tagging each object with its source context via the
// render.SourceContextAnnotation annotation. Context-aware reads and mutations
// route to the child named by internal.KeyScopeContext; unscoped calls keep the
// primary/single-context behavior.
type MultiFactory struct {
	children            map[string]childFactory
	primary             string
	mx                  sync.RWMutex
	childListTimeout    time.Duration
	lastTickErrors      []ContextError
	lastTickDivergences []ContextError
	// flashedDivergences tracks (gvr, ctx) pairs already surfaced via the
	// banner so the one-shot dedup survives across ticks. Keyed by
	// gvr.String() + "@" + ctxName.
	flashedDivergences map[string]struct{}
	// health tracks the rolling per-context health state machine that drives
	// the quarantine banner. Updated at the end of each listImpl tick.
	health                  map[string]*childHealthState
	pendingTransitions      []ContextHealthTransition
	quarantineProbeInterval time.Duration
}

// NewMultiFactory builds a MultiFactory from a set of already-constructed
// per-context factories. primary must be a key in children — it is the
// kubeconfig's current-context and is used for non-fan-out operations
// (Get, AddForwarder, Client, etc.).
func NewMultiFactory(primary string, children map[string]*Factory) (*MultiFactory, error) {
	if _, ok := children[primary]; !ok {
		return nil, fmt.Errorf("primary context %q not present in children", primary)
	}
	cc := make(map[string]childFactory, len(children))
	for k, v := range children {
		cc[k] = v
	}
	return &MultiFactory{
		children:                cc,
		primary:                 primary,
		childListTimeout:        defaultChildListTimeout,
		flashedDivergences:      make(map[string]struct{}),
		health:                  initHealth(cc),
		quarantineProbeInterval: defaultQuarantineProbeInterval,
	}, nil
}

// newMultiFactoryForTesting is an internal constructor used by tests in this
// package to plug in fake childFactory implementations. Not exported.
func newMultiFactoryForTesting(primary string, children map[string]childFactory) (*MultiFactory, error) {
	if _, ok := children[primary]; !ok {
		return nil, fmt.Errorf("primary context %q not present in children", primary)
	}
	return &MultiFactory{
		children:                children,
		primary:                 primary,
		childListTimeout:        defaultChildListTimeout,
		flashedDivergences:      make(map[string]struct{}),
		health:                  initHealth(children),
		quarantineProbeInterval: defaultQuarantineProbeInterval,
	}, nil
}

// initHealth seeds the per-context health map at construction time so the
// state machine doesn't need lazy nil checks on the hot path. Every child
// starts Healthy.
func initHealth(children map[string]childFactory) map[string]*childHealthState {
	out := make(map[string]*childHealthState, len(children))
	for name := range children {
		out[name] = &childHealthState{state: HealthHealthy}
	}
	return out
}

// SetChildListTimeout overrides the per-child List deadline. Exposed so tests
// can shorten the wait; production callers can leave the default in place.
func (m *MultiFactory) SetChildListTimeout(d time.Duration) {
	m.mx.Lock()
	defer m.mx.Unlock()
	if d > 0 {
		m.childListTimeout = d
	}
}

// SetQuarantineProbeInterval overrides the recovery-probe cadence for
// quarantined children. Exposed so tests can compress the wait.
func (m *MultiFactory) SetQuarantineProbeInterval(d time.Duration) {
	m.mx.Lock()
	defer m.mx.Unlock()
	if d > 0 {
		m.quarantineProbeInterval = d
	}
}

// LastTickErrors returns a copy of the per-child errors captured during the
// most recent List fan-out. Empty when the last tick succeeded across every
// child. Used by the view layer to surface partial-failure banners without
// poisoning the merged row set. Excludes IsNotFound — those are routed to
// LastTickDivergences instead.
func (m *MultiFactory) LastTickErrors() []ContextError {
	m.mx.RLock()
	defer m.mx.RUnlock()
	if len(m.lastTickErrors) == 0 {
		return nil
	}
	out := make([]ContextError, len(m.lastTickErrors))
	copy(out, m.lastTickErrors)
	return out
}

// LastTickDivergences returns the per-child errors where the resource type
// (GVR) was missing on that cluster. Treated as legitimately-empty rather
// than failed: counted toward success so the merged set ships normally.
// Surfaced once via NewDivergencesForFlash to warn the user that they're
// viewing partial data.
func (m *MultiFactory) LastTickDivergences() []ContextError {
	m.mx.RLock()
	defer m.mx.RUnlock()
	if len(m.lastTickDivergences) == 0 {
		return nil
	}
	out := make([]ContextError, len(m.lastTickDivergences))
	copy(out, m.lastTickDivergences)
	return out
}

// recordTickHealthLocked applies one child's per-tick outcome to the health
// state machine. err==nil counts as a success (resets the fail streak and
// recovers a quarantined child); a non-nil err increments the streak and
// flips to Quarantined once it crosses quarantineThreshold. Transitions are
// recorded in pendingTransitions for the view layer to drain via
// NewHealthTransitions. The caller must hold m.mx.
func (m *MultiFactory) recordTickHealthLocked(ctx string, err error) {
	h := m.health[ctx]
	if h == nil {
		h = &childHealthState{state: HealthHealthy}
		m.health[ctx] = h
	}
	prev := h.state
	if err == nil {
		h.fails = 0
		h.lastErr = nil
		if prev == HealthQuarantined {
			h.state = HealthHealthy
			m.pendingTransitions = append(m.pendingTransitions, ContextHealthTransition{
				Context: ctx, From: prev, To: HealthHealthy, At: time.Now(),
			})
		}
		return
	}
	h.fails++
	h.lastErr = err
	if prev == HealthHealthy && h.fails >= quarantineThreshold {
		h.state = HealthQuarantined
		// Seed lastProbeAt with the transition time so the next tick falls
		// inside the probe-interval silence window — the cluster just failed
		// quarantineThreshold times in a row, no point re-probing immediately.
		h.lastProbeAt = time.Now()
		m.pendingTransitions = append(m.pendingTransitions, ContextHealthTransition{
			Context: ctx, From: prev, To: HealthQuarantined, Err: err, At: h.lastProbeAt,
		})
	}
}

// HealthSnapshot returns a point-in-time copy of every child's health state.
// Used by the cluster-info panel and tests.
func (m *MultiFactory) HealthSnapshot() map[string]ClusterHealth {
	m.mx.RLock()
	defer m.mx.RUnlock()
	out := make(map[string]ClusterHealth, len(m.health))
	for name, h := range m.health {
		out[name] = h.state
	}
	return out
}

// HealthSummary collapses HealthSnapshot into a tally + the sorted list of
// currently-quarantined contexts. Used by the cluster-info panel which wants
// a one-line "M/N healthy" summary rather than a per-context map.
func (m *MultiFactory) HealthSummary() (total, healthy int, quarantined []string) {
	m.mx.RLock()
	defer m.mx.RUnlock()
	total = len(m.health)
	for name, h := range m.health {
		if h.state == HealthHealthy {
			healthy++
			continue
		}
		quarantined = append(quarantined, name)
	}
	slices.Sort(quarantined)
	return
}

// NewHealthTransitions drains and returns the Healthy↔Quarantined edges that
// have occurred since the last call. The view layer calls this once per
// refresh tick to dispatch transition flashes. Returning a fresh slice keeps
// the dispatch idempotent — a transition is surfaced exactly once.
func (m *MultiFactory) NewHealthTransitions() []ContextHealthTransition {
	m.mx.Lock()
	defer m.mx.Unlock()
	if len(m.pendingTransitions) == 0 {
		return nil
	}
	out := m.pendingTransitions
	m.pendingTransitions = nil
	return out
}

// NewDivergencesForFlash returns the subset of last-tick divergences for gvr
// that have not yet been surfaced via the banner, and atomically marks them
// as acknowledged so subsequent calls return only newly-discovered ones.
// One-shot semantics scoped to (gvr, ctx) — re-fires only if the MultiFactory
// is rebuilt (context-switch) or a *new* context starts diverging.
//
// Called by the view layer once per refresh tick after List returns.
func (m *MultiFactory) NewDivergencesForFlash(gvr *client.GVR) []string {
	m.mx.Lock()
	defer m.mx.Unlock()
	var out []string
	for _, d := range m.lastTickDivergences {
		key := gvr.String() + "@" + d.Context
		if _, seen := m.flashedDivergences[key]; seen {
			continue
		}
		m.flashedDivergences[key] = struct{}{}
		out = append(out, d.Context)
	}
	return out
}

// Client returns the primary child's connection. In multi-context mode the
// view layer uses this for discovery, version, namespace validation, etc. —
// all anchored to the primary context.
func (m *MultiFactory) Client() client.Connection {
	m.mx.RLock()
	defer m.mx.RUnlock()
	return m.children[m.primary].Client()
}

// HasMetrics reports whether every child cluster exposes a metrics-server.
// Conservative AND across children: in multi-context mode CPU/MEM columns are
// only meaningful if every cluster can supply numbers, otherwise rows from
// metric-less clusters would render as N/A and visually skew comparisons.
func (m *MultiFactory) HasMetrics() bool {
	m.mx.RLock()
	defer m.mx.RUnlock()
	for _, c := range m.children {
		if !c.Client().HasMetrics() {
			return false
		}
	}
	return true
}

// Contexts returns the sorted list of child context names. Used by the view
// layer to emit a startup summary flash and to power the cluster-info panel.
func (m *MultiFactory) Contexts() []string {
	m.mx.RLock()
	defer m.mx.RUnlock()
	return slices.Sorted(maps.Keys(m.children))
}

// ClientFor returns the client.Connection for the child identified by
// internal.KeyScopeContext on ctx, falling back to the primary's connection
// when the key is absent / empty / unknown. DAOs use this to dispatch
// mutations (delete, restart, scale, …) to the row's source cluster instead
// of always hitting the primary.
func (m *MultiFactory) ClientFor(ctx context.Context) client.Connection {
	m.mx.RLock()
	defer m.mx.RUnlock()
	if scope, ok := ctx.Value(internal.KeyScopeContext).(string); ok && scope != "" {
		if child, exists := m.children[scope]; exists {
			return child.Client()
		}
	}
	return m.children[m.primary].Client()
}

// List fans out across all children in parallel, deep-copies each returned
// object, and tags it with the source-context annotation. The merged slice is
// returned. The first error encountered aborts the whole call (demo behavior;
// MVP will isolate per-context errors).
func (m *MultiFactory) List(gvr *client.GVR, ns string, wait bool, sel labels.Selector) ([]runtime.Object, error) {
	return m.listImpl(nil, gvr, ns, wait, sel)
}

// ListWithContext is the context-aware variant. When internal.KeyScopeContext
// is present as a non-empty string in ctx, fan-out is constrained to that one
// child context (used by drill-down navigation so child views surface
// resources only from the parent row's source cluster). Otherwise behaves
// like List.
func (m *MultiFactory) ListWithContext(ctx context.Context, gvr *client.GVR, ns string, wait bool, sel labels.Selector) ([]runtime.Object, error) {
	return m.listImpl(ctx, gvr, ns, wait, sel)
}

func (m *MultiFactory) listImpl(ctx context.Context, gvr *client.GVR, ns string, wait bool, sel labels.Selector) ([]runtime.Object, error) {
	m.mx.RLock()

	type childResult struct {
		ctxName string
		objs    []runtime.Object
		err     error
	}

	// Iterate children in a deterministic order so the merged row sequence is
	// stable across refresh ticks (avoids visible row reshuffling).
	ctxNames := slices.Sorted(maps.Keys(m.children))
	// Honor KeyScopeContext: constrain fan-out to a single child if set.
	// Scope-targeted calls bypass quarantine skipping below — the user (or
	// drill-down) explicitly asked for THIS cluster, so we attempt it even
	// if it's currently flagged unhealthy.
	scopeTargeted := false
	if ctx != nil {
		if scope, ok := ctx.Value(internal.KeyScopeContext).(string); ok && scope != "" {
			if _, exists := m.children[scope]; exists {
				ctxNames = []string{scope}
				scopeTargeted = true
			} else {
				m.mx.RUnlock()
				return nil, fmt.Errorf("scope context %q not in children", scope)
			}
		}
	}
	timeout := m.childListTimeout
	// Drop the read lock and re-acquire write to update probe timestamps
	// on quarantined children we're about to attempt.
	m.mx.RUnlock()
	m.mx.Lock()
	now := time.Now()
	attemptable := make([]string, 0, len(ctxNames))
	skipped := make([]string, 0)
	probeInterval := m.quarantineProbeInterval
	for _, n := range ctxNames {
		h := m.health[n]
		if !scopeTargeted && h != nil && h.state == HealthQuarantined && now.Sub(h.lastProbeAt) < probeInterval {
			skipped = append(skipped, n)
			continue
		}
		if h != nil && h.state == HealthQuarantined {
			h.lastProbeAt = now
		}
		attemptable = append(attemptable, n)
	}
	snapshot := make(map[string]childFactory, len(attemptable))
	for _, n := range attemptable {
		snapshot[n] = m.children[n]
	}
	m.mx.Unlock()
	// Replace ctxNames with the attempt list. Skipped children stay in
	// their current health state — their counters don't move, the next
	// probe window will refresh.
	ctxNames = attemptable
	_ = skipped // reserved for future telemetry; quarantined-skip is silent today

	results := make([]childResult, 0, len(ctxNames))
	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	for _, ctxName := range ctxNames {
		child := snapshot[ctxName]
		wg.Add(1)
		go func(name string, c childFactory) {
			defer wg.Done()
			// Run the child's List in an inner goroutine so a stuck cluster
			// (e.g. blocked on informer sync against an unreachable API
			// server) doesn't stall the whole fan-out tick. The inner
			// goroutine is allowed to leak past the deadline — it'll finish
			// or be GC'd when the underlying List eventually returns. This
			// trades a possible goroutine leak under sustained outage for
			// UI responsiveness, which is the right call for a TUI.
			type inner struct {
				objs []runtime.Object
				err  error
			}
			ch := make(chan inner, 1)
			go func() {
				oo, err := c.List(gvr, ns, wait, sel)
				ch <- inner{objs: oo, err: err}
			}()
			var r childResult
			r.ctxName = name
			select {
			case res := <-ch:
				r.objs, r.err = res.objs, res.err
			case <-time.After(timeout):
				r.err = fmt.Errorf("list timed out after %s", timeout)
			}
			mu.Lock()
			results = append(results, r)
			mu.Unlock()
		}(ctxName, child)
	}
	wg.Wait()

	// Sort results back into deterministic order — goroutines complete in
	// arbitrary order so the slice must be re-ordered to match ctxNames.
	slices.SortFunc(results, func(a, b childResult) int {
		switch {
		case a.ctxName < b.ctxName:
			return -1
		case a.ctxName > b.ctxName:
			return 1
		default:
			return 0
		}
	})

	var (
		merged      []runtime.Object
		errs        []ContextError
		divergences []ContextError
		successes   int
	)
	for _, r := range results {
		if r.err != nil {
			if apierrors.IsNotFound(r.err) {
				// Discovery divergence: cluster doesn't have this GVR. Treat
				// as legitimately-empty (counts toward success) but route to
				// the divergence channel so the user sees a one-shot banner.
				divergences = append(divergences, ContextError{Context: r.ctxName, Err: r.err})
				successes++
				continue
			}
			errs = append(errs, ContextError{Context: r.ctxName, Err: r.err})
			continue
		}
		successes++
		for _, o := range r.objs {
			tagged, err := tagWithSourceContext(o, r.ctxName)
			if err != nil {
				errs = append(errs, ContextError{Context: r.ctxName, Err: err})
				continue
			}
			merged = append(merged, tagged)
		}
	}

	// Stash for the view layer to surface via flash/banner.
	m.mx.Lock()
	m.lastTickErrors = errs
	m.lastTickDivergences = divergences
	// Drive the per-context health state machine off this tick's outcomes.
	// Divergences (IsNotFound) count toward health: that cluster IS up, it
	// just doesn't have this GVR.
	failed := make(map[string]error, len(errs))
	for _, e := range errs {
		failed[e.Context] = e.Err
	}
	for _, name := range ctxNames {
		m.recordTickHealthLocked(name, failed[name])
	}
	m.mx.Unlock()

	// Partial success: at least one child returned rows. Don't poison the
	// merged set with an error — the caller would treat it as fatal and the
	// UI would go blank. Per-context errors are still available via
	// LastTickErrors for side-channel surfacing.
	if successes > 0 {
		return merged, nil
	}
	if len(errs) == 0 {
		return merged, nil
	}
	joined := make([]error, 0, len(errs))
	for _, e := range errs {
		joined = append(joined, fmt.Errorf("context %q: %w", e.Context, e.Err))
	}
	return nil, errors.Join(joined...)
}

// tagWithSourceContext deep-copies the object (to avoid mutating the informer
// cache) and stamps it with the source-context annotation.
func tagWithSourceContext(o runtime.Object, ctxName string) (runtime.Object, error) {
	u, ok := o.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("expected *unstructured.Unstructured, got %T", o)
	}
	copy := u.DeepCopy()
	ann := copy.GetAnnotations()
	if ann == nil {
		ann = make(map[string]string, 1)
	}
	ann[render.SourceContextAnnotation] = ctxName
	copy.SetAnnotations(ann)
	return copy, nil
}

// Get delegates to the primary child. Used by code paths that don't carry a
// context.Context; callers that have one should prefer GetWithContext to honor
// internal.KeyScopeContext for per-row routing.
func (m *MultiFactory) Get(gvr *client.GVR, path string, wait bool, sel labels.Selector) (runtime.Object, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()
	return m.children[m.primary].Get(gvr, path, wait, sel)
}

// GetWithContext routes to the child identified by internal.KeyScopeContext.
// Falls back to the primary when the key is absent or empty. Returned objects
// are tagged with the source-context annotation so renderers can populate
// row.Source consistently with the List path.
func (m *MultiFactory) GetWithContext(ctx context.Context, gvr *client.GVR, path string, wait bool, sel labels.Selector) (runtime.Object, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()

	target := m.primary
	if scope, ok := ctx.Value(internal.KeyScopeContext).(string); ok && scope != "" {
		if _, exists := m.children[scope]; !exists {
			return nil, fmt.Errorf("scope context %q not in children", scope)
		}
		target = scope
	}
	o, err := m.children[target].Get(gvr, path, wait, sel)
	if err != nil {
		return nil, err
	}
	return tagWithSourceContext(o, target)
}

// ForResource delegates to the primary. Used by `List` internals on the
// primary path; the fan-out List in this type calls each child's List
// directly, so non-primary informers are still backed by their own children's
// `ForResource`.
func (m *MultiFactory) ForResource(ns string, gvr *client.GVR) (informers.GenericInformer, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()
	return m.children[m.primary].ForResource(ns, gvr)
}

// CanForResource delegates to the primary (RBAC check anchored to primary).
func (m *MultiFactory) CanForResource(ns string, gvr *client.GVR, verbs []string) (informers.GenericInformer, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()
	return m.children[m.primary].CanForResource(ns, gvr, verbs)
}

// WaitForCacheSync waits on every child's informers.
func (m *MultiFactory) WaitForCacheSync() {
	m.mx.RLock()
	defer m.mx.RUnlock()
	for _, c := range m.children {
		c.WaitForCacheSync()
	}
}

// HasSynced returns true only when every child reports synced for the given
// resource/namespace. Any child error short-circuits and propagates.
func (m *MultiFactory) HasSynced(gvr *client.GVR, ns string) (bool, error) {
	m.mx.RLock()
	defer m.mx.RUnlock()
	for ctxName, c := range m.children {
		ok, err := c.HasSynced(gvr, ns)
		if err != nil {
			return false, fmt.Errorf("context %q: %w", ctxName, err)
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// Start starts informers in every child for the given namespace.
func (m *MultiFactory) Start(ns string) {
	m.mx.RLock()
	defer m.mx.RUnlock()
	for ctxName, c := range m.children {
		slog.Debug("MultiFactory: starting informers", slogs.Context, ctxName, slogs.Namespace, ns)
		c.Start(ns)
	}
}

// Terminate stops informers in every child.
func (m *MultiFactory) Terminate() {
	m.mx.RLock()
	defer m.mx.RUnlock()
	for ctxName, c := range m.children {
		slog.Debug("MultiFactory: terminating informers", slogs.Context, ctxName)
		c.Terminate()
	}
}

// SetActiveNS sets the active namespace on every child. All children get the
// same namespace (uniform-namespace assumption — Option A in the design doc).
func (m *MultiFactory) SetActiveNS(ns string) error {
	m.mx.RLock()
	defer m.mx.RUnlock()
	var errs []error
	for ctxName, c := range m.children {
		if err := c.SetActiveNS(ns); err != nil {
			errs = append(errs, fmt.Errorf("context %q: %w", ctxName, err))
		}
	}
	return errors.Join(errs...)
}

// AddForwarder registers a port forwarder with the primary. Port-forwards
// aren't supported on non-primary rows in the demo.
func (m *MultiFactory) AddForwarder(pf Forwarder) {
	m.mx.RLock()
	defer m.mx.RUnlock()
	m.children[m.primary].AddForwarder(pf)
}

// ForwarderFor delegates to the primary.
func (m *MultiFactory) ForwarderFor(path string) (Forwarder, bool) {
	m.mx.RLock()
	defer m.mx.RUnlock()
	return m.children[m.primary].ForwarderFor(path)
}

// DeleteForwarder deletes a port forwarder from the primary.
func (m *MultiFactory) DeleteForwarder(path string) {
	m.mx.RLock()
	defer m.mx.RUnlock()
	m.children[m.primary].DeleteForwarder(path)
}

// Forwarders returns the primary's port forwarders.
func (m *MultiFactory) Forwarders() Forwarders {
	m.mx.RLock()
	defer m.mx.RUnlock()
	return m.children[m.primary].Forwarders()
}

// ValidatePortForwards validates port forwarders on the primary.
func (m *MultiFactory) ValidatePortForwards() {
	m.mx.RLock()
	defer m.mx.RUnlock()
	m.children[m.primary].ValidatePortForwards()
}
