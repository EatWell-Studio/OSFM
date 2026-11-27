package main

// The internal ID scan's probes of the per-layer exports (internal/api/layer_export.go, T041). Add a probe for every route the feature registers,
// with the routes only maintainers may use in maintainerOnly; see registerProbes.
func init() {
	registerProbes(probeSet{})
}
