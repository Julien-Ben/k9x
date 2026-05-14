package client

import "k8s.io/apimachinery/pkg/util/sets"

var (
	// Apps...
	DpGVR  = NewGVR("apps/v1/deployments")
	StsGVR = NewGVR("apps/v1/statefulsets")
	DsGVR  = NewGVR("apps/v1/daemonsets")
	RsGVR  = NewGVR("apps/v1/replicasets")
	RcGVR  = NewGVR("apps/v1/replicationcontrollers")

	// Core...
	SaGVR   = NewGVR("v1/serviceaccounts")
	PvcGVR  = NewGVR("v1/persistentvolumeclaims")
	PvGVR   = NewGVR("v1/persistentvolumes")
	CmGVR   = NewGVR("v1/configmaps")
	SecGVR  = NewGVR("v1/secrets")
	EvGVR   = NewGVR("events.k8s.io/v1/events")
	EpGVR   = NewGVR("v1/endpoints")
	PodGVR  = NewGVR("v1/pods")
	NsGVR   = NewGVR("v1/namespaces")
	NodeGVR = NewGVR("v1/nodes")
	SvcGVR  = NewGVR("v1/services")

	// Discovery...
	EpsGVR = NewGVR("discovery.k8s.io/v1/endpointslices")

	// Autoscaling...
	HpaGVR = NewGVR("autoscaling/v1/horizontalpodautoscalers")

	// Batch...
	CjGVR  = NewGVR("batch/v1/cronjobs")
	JobGVR = NewGVR("batch/v1/jobs")

	// Misc...
	CrdGVR = NewGVR("apiextensions.k8s.io/v1/customresourcedefinitions")
	PcGVR  = NewGVR("scheduling.k8s.io/v1/priorityclasses")
	NpGVR  = NewGVR("networking.k8s.io/v1/networkpolicies")
	ScGVR  = NewGVR("storage.k8s.io/v1/storageclasses")

	// Policy...
	PdbGVR = NewGVR("policy/v1/poddisruptionbudgets")
	PspGVR = NewGVR("policy/v1beta1/podsecuritypolicies")

	IngGVR = NewGVR("networking.k8s.io/v1/ingresses")

	// Metrics...
	NmxGVR = NewGVR("metrics.k8s.io/v1beta1/nodes")
	PmxGVR = NewGVR("metrics.k8s.io/v1beta1/pods")

	// K9s...
	CpuGVR = NewGVR("cpu")
	MemGVR = NewGVR("memory")
	WkGVR  = NewGVR("workloads")
	CoGVR  = NewGVR("containers")
	CtGVR  = NewGVR("contexts")
	RefGVR = NewGVR("references")
	PuGVR  = NewGVR("pulses")
	ScnGVR = NewGVR("scans")
	DirGVR = NewGVR("dirs")
	PfGVR  = NewGVR("portforwards")
	SdGVR  = NewGVR("screendumps")
	BeGVR  = NewGVR("benchmarks")
	AliGVR = NewGVR("aliases")
	XGVR   = NewGVR("xrays")
	HlpGVR = NewGVR("help")
	QGVR   = NewGVR("quit")

	// Helm...
	HmGVR  = NewGVR("helm")
	HmhGVR = NewGVR("helm-history")

	// RBAC...
	RbacGVR = NewGVR("rbac")
	PolGVR  = NewGVR("policy")
	UsrGVR  = NewGVR("users")
	GrpGVR  = NewGVR("groups")
	CrGVR   = NewGVR("rbac.authorization.k8s.io/v1/clusterroles")
	CrbGVR  = NewGVR("rbac.authorization.k8s.io/v1/clusterrolebindings")
	RoGVR   = NewGVR("rbac.authorization.k8s.io/v1/roles")
	RobGVR  = NewGVR("rbac.authorization.k8s.io/v1/rolebindings")
)

var reservedGVRs = sets.New(
	CpuGVR,
	MemGVR,
	WkGVR,
	CoGVR,
	CtGVR,
	RefGVR,
	PuGVR,
	ScnGVR,
	DirGVR,
	PfGVR,
	SdGVR,
	BeGVR,
	AliGVR,
	XGVR,
	HlpGVR,
	QGVR,
	HmGVR,
	HmhGVR,
	RbacGVR,
	PolGVR,
	UsrGVR,
	GrpGVR,
)

// nestedGVRs are synthesized / drill-down views whose rows are by
// definition single-cluster (they require a parent selection or hold
// app-local state). In multi-context mode the CONTEXT column would
// render with empty cells for these, so MultiContextRenderer is not
// applied. Kept narrow on purpose: top-level reserved views like
// workloads / contexts / pulses still get the column.
var nestedGVRs = sets.New(
	CoGVR,   // containers - drill into a pod
	RefGVR,  // references - synthesized scan
	ScnGVR,  // scans - synthesized
	DirGVR,  // dirs - file directory
	PfGVR,   // portforwards - app state
	SdGVR,   // screendumps - app state
	BeGVR,   // benchmarks - app state
	HmhGVR,  // helm-history - drill into a helm release
)

// IsNestedGVR reports whether gvr is a synthesized / drill-down view
// whose rows do not carry a source-cluster annotation. Used by the
// multi-context renderer gate to skip prepending the CONTEXT column on
// nested table models (e.g. container view inside a pod).
func IsNestedGVR(gvr *GVR) bool {
	return nestedGVRs.Has(gvr)
}
