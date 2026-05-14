// SPDX-License-Identifier: Apache-2.0
// Copyright Authors of K9s

package internal

// ContextKey represents context key.
type ContextKey string

// A collection of context keys.
const (
	KeyFactory       ContextKey = "factory"
	KeyLabels        ContextKey = "labels"
	KeyFields        ContextKey = "fields"
	KeyTable         ContextKey = "table"
	KeyDir           ContextKey = "dir"
	KeyPath          ContextKey = "path"
	KeySubject       ContextKey = "subject"
	KeyGVR           ContextKey = "gvr"
	KeyFQN           ContextKey = "fqn"
	KeyForwards      ContextKey = "forwards"
	KeyContainers    ContextKey = "containers"
	KeyBenchCfg      ContextKey = "benchcfg"
	KeyAliases       ContextKey = "aliases"
	KeyUID           ContextKey = "uid"
	KeySubjectKind   ContextKey = "subjectKind"
	KeySubjectName   ContextKey = "subjectName"
	KeyNamespace     ContextKey = "namespace"
	KeyCluster       ContextKey = "cluster"
	KeyApp           ContextKey = "app"
	KeyStyles        ContextKey = "styles"
	KeyMetrics       ContextKey = "metrics"
	KeyHasMetrics    ContextKey = "has-metrics"
	KeyToast         ContextKey = "toast"
	KeyWithMetrics   ContextKey = "withMetrics"
	KeyViewConfig    ContextKey = "viewConfig"
	KeyWait          ContextKey = "wait"
	KeyPodCounting   ContextKey = "podCounting"
	KeyEnableImgScan ContextKey = "vulScan"
	KeyMultiContext  ContextKey = "multiContext"
	// KeyScopeContext, when present and non-empty in a context.Context,
	// constrains MultiFactory.List/Get fan-out to that single child context.
	// Used by drill-down navigation so a child view (e.g. Deploy → Pods) only
	// surfaces resources from the parent row's source cluster.
	KeyScopeContext ContextKey = "scopeContext"
)
