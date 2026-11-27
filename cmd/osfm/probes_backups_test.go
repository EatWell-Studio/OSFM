package main

// The internal ID scan's probes of backups (internal/api/backups.go, T042). Add a probe for every route the feature registers,
// with the routes only maintainers may use in maintainerOnly; see registerProbes.
func init() {
	registerProbes(probeSet{})
}
