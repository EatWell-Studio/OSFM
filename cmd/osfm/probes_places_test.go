package main

// The internal ID scan's probes of creating places and recommendations (internal/api/places.go, T026). Add a probe for every route the feature registers,
// with the routes only maintainers may use in maintainerOnly; see registerProbes.
func init() {
	registerProbes(probeSet{})
}
