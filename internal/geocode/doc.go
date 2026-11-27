// Package geocode is the server-side proxy for place-name search (B7, AD11, architecture §3.7): the web app sends a
// search term and a city to this instance, and the instance asks the upstream search services, so that the member's
// browser never talks to a third party (E5).
//
// A search goes to Photon first, bounded to the city; when Photon fails (an error, a timeout, an unsuccessful status
// or an unreadable answer), it goes to Nominatim with viewbox and bounded=1. Either way the results are filtered to
// the city again and reduced to name, label and position. When both fail, [Geocoder.Search] returns [ErrUnavailable].
//
// What leaves the server is the search term, the city's bounding box and a User-Agent naming OSFM, its version and
// the contact the maintainers set: never the member's cookies, address, language or other request headers. The usage
// policies of the public services (one request per second at most to Nominatim, no search as you type, caching,
// identification) are kept here: all requests to Nominatim of the whole instance are spaced by
// [Options].NominatimInterval, and answers are cached in memory (least recently used, for [Options].CacheTTL; never in
// the database, M2-Q13). The per-member budget is the caller's (internal/api).
//
// Nothing here logs a search term: an upstream failure is logged with the service and the kind of failure only. An
// error of net/http names the request URL, which holds the term, so it is never logged or returned as it is.
//
// The package knows nothing about PocketBase or HTTP routes; internal/api serves it.
package geocode
