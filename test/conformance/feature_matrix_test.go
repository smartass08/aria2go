package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/smartass08/aria2go/internal/config"
	"github.com/smartass08/aria2go/internal/rpc/dispatcher"
)

type featureMatrixFile struct {
	SchemaVersion            int                  `json:"schema_version"`
	StatusValues             []string             `json:"status_values"`
	OptionInventory          map[string][]string  `json:"option_inventory"`
	RPCInventory             map[string][]string  `json:"rpc_inventory"`
	RPCNotificationInventory map[string][]string  `json:"rpc_notification_inventory"`
	SourceOptionExceptions   map[string]string    `json:"source_option_exceptions"`
	Entries                  []featureMatrixEntry `json:"entries"`
}

type featureMatrixEntry struct {
	ID                  string   `json:"id"`
	Area                string   `json:"area"`
	Feature             string   `json:"feature"`
	Status              string   `json:"status"`
	SourceTruth         []string `json:"source_truth"`
	Evidence            []string `json:"evidence"`
	ConformanceCoverage []string `json:"conformance_coverage"`
	UnitCoverage        []string `json:"unit_coverage"`
	Notes               string   `json:"notes"`
}

func TestFeatureMatrixClaimsAreCovered(t *testing.T) {
	root := featureMatrixProjectRoot(t)
	matrix := loadFeatureMatrix(t, root)

	if matrix.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d, want 1", matrix.SchemaVersion)
	}
	if len(matrix.Entries) == 0 {
		t.Fatal("feature matrix has no entries")
	}

	allowed := map[string]bool{}
	for _, status := range matrix.StatusValues {
		allowed[status] = true
	}
	if len(allowed) == 0 {
		t.Fatal("feature matrix declares no status values")
	}

	seenIDs := map[string]bool{}
	var implemented, partial, missing, testsOnly, notApplicable int
	for i, entry := range matrix.Entries {
		label := fmt.Sprintf("entries[%d]", i)
		requireMatrixField(t, label, "id", entry.ID)
		requireMatrixField(t, label, "area", entry.Area)
		requireMatrixField(t, label, "feature", entry.Feature)
		requireMatrixField(t, label, "status", entry.Status)
		requireMatrixField(t, label, "notes", entry.Notes)

		if seenIDs[entry.ID] {
			t.Fatalf("duplicate feature matrix id %q", entry.ID)
		}
		seenIDs[entry.ID] = true

		if !allowed[entry.Status] {
			t.Fatalf("%s %q has unsupported status %q", label, entry.ID, entry.Status)
		}

		if entry.Status != "not-applicable" {
			if len(entry.SourceTruth) == 0 {
				t.Fatalf("%s %q has no source_truth refs", label, entry.ID)
			}
			if len(entry.Evidence) == 0 {
				t.Fatalf("%s %q has no implementation evidence refs", label, entry.ID)
			}
		}

		validateMatrixRefs(t, root, entry.ID, "source_truth", entry.SourceTruth, false)
		validateMatrixRefs(t, root, entry.ID, "evidence", entry.Evidence, false)
		validateMatrixRefs(t, root, entry.ID, "conformance_coverage", entry.ConformanceCoverage, true)
		validateMatrixRefs(t, root, entry.ID, "unit_coverage", entry.UnitCoverage, false)

		switch entry.Status {
		case "implemented":
			implemented++
			if len(entry.ConformanceCoverage) == 0 {
				t.Fatalf("%q is marked implemented without conformance_coverage", entry.ID)
			}
			for _, ref := range entry.ConformanceCoverage {
				path := matrixRefPath(ref)
				if !strings.HasPrefix(path, "test/conformance/") {
					t.Fatalf("%q implemented coverage %q is not under test/conformance", entry.ID, ref)
				}
			}
		case "partial":
			partial++
		case "missing":
			missing++
			if len(entry.ConformanceCoverage) != 0 {
				t.Fatalf("%q is missing but has conformance_coverage", entry.ID)
			}
		case "tests-only":
			testsOnly++
			if len(entry.UnitCoverage) == 0 && len(entry.ConformanceCoverage) == 0 {
				t.Fatalf("%q is tests-only but has no coverage refs", entry.ID)
			}
		case "not-applicable":
			notApplicable++
		}
	}

	t.Logf("feature matrix: implemented=%d partial=%d missing=%d tests-only=%d not-applicable=%d",
		implemented, partial, missing, testsOnly, notApplicable)
}

func TestFeatureMatrixCoversGoOptionInventory(t *testing.T) {
	root := featureMatrixProjectRoot(t)
	matrix := loadFeatureMatrix(t, root)
	entryIDs := featureMatrixEntryIDs(matrix)
	covered := flattenMatrixInventory(t, "option_inventory", matrix.OptionInventory, entryIDs)

	goOptions := config.Default().Fields()
	goOptionSet := stringSet(goOptions)
	for option := range covered {
		if !goOptionSet[option] {
			t.Fatalf("option_inventory references unknown Go option %q", option)
		}
	}

	var missing []string
	for _, option := range goOptions {
		if !covered[option] {
			missing = append(missing, option)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("feature matrix does not classify Go config options: %s", strings.Join(missing, ", "))
	}
}

func TestFeatureMatrixCoversRPCMethodInventory(t *testing.T) {
	root := featureMatrixProjectRoot(t)
	matrix := loadFeatureMatrix(t, root)
	entryIDs := featureMatrixEntryIDs(matrix)
	covered := flattenMatrixInventory(t, "rpc_inventory", matrix.RPCInventory, entryIDs)

	methods := dispatcher.New(nil, dispatcher.Config{}).ListMethods()
	methodSet := stringSet(methods)
	for method := range covered {
		if !methodSet[method] {
			t.Fatalf("rpc_inventory references unknown Go RPC method %q", method)
		}
	}

	var missing []string
	for _, method := range methods {
		if !covered[method] {
			missing = append(missing, method)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("feature matrix does not classify Go RPC methods: %s", strings.Join(missing, ", "))
	}
}

func TestFeatureMatrixCoversRPCNotificationInventory(t *testing.T) {
	root := featureMatrixProjectRoot(t)
	matrix := loadFeatureMatrix(t, root)
	entryIDs := featureMatrixEntryIDs(matrix)
	covered := flattenMatrixInventory(t, "rpc_notification_inventory", matrix.RPCNotificationInventory, entryIDs)

	notifications := dispatcher.New(nil, dispatcher.Config{}).ListNotifications()
	notificationSet := stringSet(notifications)
	for notification := range covered {
		if !notificationSet[notification] {
			t.Fatalf("rpc_notification_inventory references unknown Go RPC notification %q", notification)
		}
	}

	var missing []string
	for _, notification := range notifications {
		if !covered[notification] {
			missing = append(missing, notification)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("feature matrix does not classify Go RPC notifications: %s", strings.Join(missing, ", "))
	}
}

func TestGoOptionInventoryComesFromSourceTruthPrefs(t *testing.T) {
	root := featureMatrixProjectRoot(t)
	matrix := loadFeatureMatrix(t, root)
	sourcePrefs := sourceTruthPrefs(t, root)

	for _, option := range config.Default().Fields() {
		if !sourcePrefs[option] {
			t.Fatalf("Go option %q is not present in source-truth aria2 prefs.cc", option)
		}
	}

	goOptions := stringSet(config.Default().Fields())
	var unclassifiedSourcePrefs []string
	for pref := range sourcePrefs {
		if goOptions[pref] {
			continue
		}
		if strings.TrimSpace(matrix.SourceOptionExceptions[pref]) == "" {
			unclassifiedSourcePrefs = append(unclassifiedSourcePrefs, pref)
		}
	}
	slices.Sort(unclassifiedSourcePrefs)
	if len(unclassifiedSourcePrefs) > 0 {
		t.Fatalf("source-truth prefs not represented by Go options or source_option_exceptions: %q", unclassifiedSourcePrefs)
	}

	for pref := range matrix.SourceOptionExceptions {
		if !sourcePrefs[pref] {
			t.Fatalf("source_option_exceptions references unknown source-truth pref %q", pref)
		}
	}
}

func loadFeatureMatrix(t *testing.T, root string) featureMatrixFile {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(root, "test", "conformance", "feature_matrix.json"))
	if err != nil {
		t.Fatalf("read feature matrix: %v", err)
	}
	var matrix featureMatrixFile
	if err := json.Unmarshal(data, &matrix); err != nil {
		t.Fatalf("parse feature matrix: %v", err)
	}
	return matrix
}

func featureMatrixEntryIDs(matrix featureMatrixFile) map[string]bool {
	entryIDs := make(map[string]bool, len(matrix.Entries))
	for _, entry := range matrix.Entries {
		entryIDs[entry.ID] = true
	}
	return entryIDs
}

func flattenMatrixInventory(t *testing.T, field string, inventory map[string][]string, entryIDs map[string]bool) map[string]bool {
	t.Helper()
	if len(inventory) == 0 {
		t.Fatalf("feature matrix has no %s", field)
	}
	covered := map[string]bool{}
	owners := map[string]string{}
	for entryID, values := range inventory {
		if !entryIDs[entryID] {
			t.Fatalf("%s uses unknown feature matrix id %q", field, entryID)
		}
		if len(values) == 0 {
			t.Fatalf("%s entry %q is empty", field, entryID)
		}
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				t.Fatalf("%s entry %q contains an empty value", field, entryID)
			}
			if previousOwner, ok := owners[value]; ok {
				t.Fatalf("%s value %q is owned by both %q and %q", field, value, previousOwner, entryID)
			}
			covered[value] = true
			owners[value] = entryID
		}
	}
	return covered
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func sourceTruthPrefs(t *testing.T, root string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "source-truth", "aria2", "src", "prefs.cc"))
	if err != nil {
		t.Fatalf("read source-truth prefs.cc: %v", err)
	}
	re := regexp.MustCompile(`makePref\("([^"]*)"\)`)
	matches := re.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		t.Fatal("found no makePref entries in source-truth prefs.cc")
	}
	prefs := make(map[string]bool, len(matches))
	for _, match := range matches {
		prefs[match[1]] = true
	}
	return prefs
}

func requireMatrixField(t *testing.T, label, field, value string) {
	t.Helper()
	if strings.TrimSpace(value) == "" {
		t.Fatalf("%s has empty %s", label, field)
	}
}

func validateMatrixRefs(t *testing.T, root, id, field string, refs []string, requireConformance bool) {
	t.Helper()
	for _, ref := range refs {
		path := matrixRefPath(ref)
		if path == "" {
			t.Fatalf("%q has empty %s ref", id, field)
		}
		if strings.Contains(path, "://") || strings.HasPrefix(path, "manual:") || strings.HasPrefix(path, "aria2c manual:") {
			continue
		}
		if requireConformance && !strings.HasPrefix(path, "test/conformance/") {
			t.Fatalf("%q %s ref %q is not a conformance test ref", id, field, ref)
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatalf("%q %s ref %q does not point at an existing local file: %v", id, field, ref, err)
		}
	}
}

func matrixRefPath(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if i := strings.Index(ref, " "); i >= 0 {
		ref = ref[:i]
	}
	if i := strings.Index(ref, "#"); i >= 0 {
		ref = ref[:i]
	}
	if i := strings.LastIndex(ref, ":"); i >= 0 {
		if _, err := strconv.Atoi(ref[i+1:]); err == nil {
			ref = ref[:i]
		}
	}
	return strings.TrimSpace(ref)
}

func featureMatrixProjectRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find project root from %s", file)
		}
		dir = parent
	}
}

func TestFeatureMatrixStatusValuesAreCanonical(t *testing.T) {
	matrix := loadFeatureMatrix(t, featureMatrixProjectRoot(t))
	want := []string{"implemented", "missing", "not-applicable", "partial", "tests-only"}
	got := append([]string(nil), matrix.StatusValues...)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("status_values = %v, want canonical set %v", got, want)
	}
}
