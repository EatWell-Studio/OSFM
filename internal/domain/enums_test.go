package domain_test

import (
	"os"
	"regexp"
	"slices"
	"testing"

	"github.com/EatWell-Studio/OSFM/internal/domain"
	"github.com/EatWell-Studio/OSFM/internal/store"
)

// enumsFile is the web app's list of fixed values, relative to this package.
const enumsFile = "../../web/src/lib/enums.ts"

// serverLists are the canonical lists of the server, by the name of the matching list in enumsFile.
var serverLists = map[string][]string{
	"attitudes":     domain.Attitudes,
	"timeSlots":     domain.TimeSlots,
	"precisions":    domain.Precisions,
	"categories":    domain.PlaceCategories,
	"cuisines":      domain.Cuisines,
	"sourceKinds":   domain.SourceKinds,
	"visibilities":  domain.WritableVisibilities,
	"signatures":    domain.Signatures,
	"reportReasons": store.ReportReasons,
	"noticeKinds":   domain.NoticeKinds,
}

var (
	tsList   = regexp.MustCompile(`(?s)export const (\w+) = \[(.*?)\] as const`)
	tsString = regexp.MustCompile(`'([^'\\]*)'`)
)

// TestEnumsMatchWebApp: every fixed list of the web app (export const name = [...] as const in enumsFile) is the
// server's list of the same values, in the same order, and the server has a list for each. The server's lists are the
// canonical ones: what the routes accept and the forms offer cannot drift apart. The web app keeps its own literal
// lists (it needs them as TypeScript literal types and tygo copies only constants, not lists), so this test reads them.
func TestEnumsMatchWebApp(t *testing.T) {
	src, err := os.ReadFile(enumsFile)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, m := range tsList.FindAllStringSubmatch(string(src), -1) {
		name := m[1]
		found[name] = true
		var web []string
		for _, s := range tsString.FindAllStringSubmatch(m[2], -1) {
			web = append(web, s[1])
		}
		server, ok := serverLists[name]
		if !ok {
			t.Errorf("%s: %s has no server list in serverLists: add the canonical list it repeats", enumsFile, name)
			continue
		}
		if !slices.Equal(web, server) {
			t.Errorf("%s: %s is\n%q\nthe server's list is\n%q", enumsFile, name, web, server)
		}
	}
	for name := range serverLists {
		if !found[name] {
			t.Errorf("%s: no list %s (export const %s = [...] as const)", enumsFile, name, name)
		}
	}
}
