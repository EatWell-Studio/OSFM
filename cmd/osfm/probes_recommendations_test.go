package main

// The internal ID scan's probes of editing, revisions and deleting (internal/api/recommendations.go, T031). Add a probe for every route the feature registers,
// with the routes only maintainers may use in maintainerOnly; see registerProbes.
func init() {
	registerProbes(probeSet{})
}
