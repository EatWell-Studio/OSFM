package main

// The internal ID scan's probes of leaving the circle (internal/api/leave.go, T038). Add a probe for every route the feature registers,
// with the routes only maintainers may use in maintainerOnly; see registerProbes.
func init() {
	registerProbes(probeSet{})
}
