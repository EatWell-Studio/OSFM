package main

// The internal ID scan's probes of the member's profile (internal/api/profile.go, T033). Add a probe for every route the feature registers,
// with the routes only maintainers may use in maintainerOnly; see registerProbes.
func init() {
	registerProbes(probeSet{})
}
