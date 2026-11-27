package main

// The internal ID scan's probes of the map data and the place card (internal/api/map.go, T027). Add a probe for every route the feature registers,
// with the routes only maintainers may use in maintainerOnly; see registerProbes.
func init() {
	registerProbes(probeSet{})
}
