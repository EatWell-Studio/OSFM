package main

// The internal ID scan's probes of the personal data export (internal/api/personal_export.go, T040). Add a probe for every route the feature registers,
// with the routes only maintainers may use in maintainerOnly; see registerProbes.
func init() {
	registerProbes(probeSet{})
}
