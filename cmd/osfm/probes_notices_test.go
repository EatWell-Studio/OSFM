package main

// The internal ID scan's probes of the member's notices (internal/api/notices.go, T037). Add a probe for every route the feature registers,
// with the routes only maintainers may use in maintainerOnly; see registerProbes.
func init() {
	registerProbes(probeSet{})
}
