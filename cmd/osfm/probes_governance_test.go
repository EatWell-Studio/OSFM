package main

// The internal ID scan's probes of invites and governance (internal/api/governance.go, T034, T035). Add a probe for every route the feature registers,
// with the routes only maintainers may use in maintainerOnly; see registerProbes.
func init() {
	registerProbes(probeSet{})
}
