package main

// The internal ID scan's probes of hiding, reports and their review (internal/api/reports.go, T036). Add a probe for every route the feature registers,
// with the routes only maintainers may use in maintainerOnly; see registerProbes.
func init() {
	registerProbes(probeSet{})
}
