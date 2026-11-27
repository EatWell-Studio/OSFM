## What does this change and why?

<!-- Link the related issue or task, e.g. "Closes #12" or "T003". -->

## How was it tested?

<!-- Commands run, browsers / devices used, manual steps. -->

## Checklist

- [ ] `bin/check-private-paths.sh` passes (no AI-assistant configuration, internal documents, secrets, instance data or symlinks)
- [ ] No real member names, recommendations or other instance data, including in tests and screenshots
- [ ] Changes to invitations, visibility or identity are described above and covered by tests
- [ ] Map data comes only from OpenStreetMap or contributors' own knowledge, with attribution kept
- [ ] All commits are signed off (`git commit -s`)

### API and privacy self-check (N5)

For every route this change adds or changes (tick, or write "n/a" with a reason):

- [ ] **Permissions**: tested as anonymous, member, author and maintainer (`internal/testapp`); maintainer routes carry `api.MaintainersOnly`, sensitive ones `auth.RequireFresh`; objects another member may not see answer 404, as missing ones do
- [ ] **Rate limit**: write routes carry the per-member write budget (`WriteBudget`), or a budget of their own; public routes `auth.RateLimit`
- [ ] **Body limit**: request bodies are read with a size limit (`readJSON`); the X-OSFM header is required on writes (session layer)
- [ ] **Visibility**: recommendations and places are read only through `store.VisibleRecommendations` / `store.VisiblePlaces` (or `store.RecommendationVisibleTo`); counts, filters, duplicate hints and exports use only what the caller may see
- [ ] **Pseudonym**: recommendations leave the server only as `api.RecommendationView` (no author uuid, no signature kind, no external links with a pseudonym); responses, exports and logs reveal no pseudonym-to-member mapping, no reporter, no coordinates, search terms or recommendation text in logs
- [ ] **Internal IDs**: probes for every new route in `cmd/osfm/probes_<feature>_test.go`; responses use uuids only
- [ ] **Types**: `web/src/api/types.gen.ts` regenerated (`go tool tygo generate`) when `internal/api` types changed
