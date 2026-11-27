package main

// The internal ID scan's probes of place-name search (internal/api/geocode.go, T039). Add a probe for every route the feature registers,
// with the routes only maintainers may use in maintainerOnly; see registerProbes.
func init() {
	registerProbes(probeSet{})
}
