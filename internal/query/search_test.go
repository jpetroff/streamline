package query

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"streamline/internal/logmodel"
	"streamline/internal/parse"
)

func TestSearchValuesAndLineSemantics(t *testing.T) {
	record := Record{Timestamp: "2026-09-06T10:00:00Z", Severity: "error", Message: "Connection Refused",
		Fields: map[string]any{"secretKey": map[string]any{"items": []any{"CAFÉ", json.Number("9007199254740993"), true, nil, "a.b", "first\nsecond", " padded "}}},
		ID:     87654321, SourceFormat: logmodel.FormatJSON, Diagnostics: []logmodel.Diagnostic{{Code: "diagnosticOnly", Message: "metadataOnly"}},
	}
	cases := []struct {
		name, text, mode, operator string
		want                       bool
	}{
		{"default substring", "REFUSED", "", "", true},
		{"unicode", "café", "plain", "or", true},
		{"nested number", "9007199254740993", "plain", "or", true},
		{"boolean", "true", "plain", "or", true},
		{"null", "null", "plain", "or", true},
		{"normalized date", "2026-09", "plain", "or", true},
		{"normalized severity", "error", "plain", "or", true},
		{"keys excluded", "secretKey", "plain", "or", false},
		{"metadata excluded", "metadataOnly\ndiagnosticOnly\n87654321\njson", "plain", "or", false},
		{"literal punctuation", "a.b", "plain", "or", true},
		{"literal regexp", "Connection.*", "plain", "or", false},
		{"regexp", "^connection.*refused$", "regexp", "or", true},
		{"case override", "(?-i:CONNECTION)", "regexp", "or", false},
		{"OR", "missing\nrefused", "plain", "or", true},
		{"AND across fields", "refused\ncafé\ntrue", "plain", "and", true},
		{"AND missing line", "refused\nmissing", "plain", "and", false},
		{"no cross value match", "refused.*café", "regexp", "or", false},
		{"whitespace preserved", " Refused ", "plain", "or", false},
		{"padded literal", " padded ", "plain", "or", true},
		{"line endings", "\r\nrefused\r\n \t\rcafé", "plain", "and", true},
		{"escaped newline", `first\nsecond`, "regexp", "or", true},
		{"dot excludes newline", "first.second", "regexp", "or", false},
		{"empty OR", "\n \t\r", "regexp", "or", true},
		{"empty AND", "", "plain", "and", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			predicate, err := compileSearch(&SearchSpec{Text: tc.text, Mode: tc.mode, Operator: tc.operator})
			if err != nil {
				t.Fatal(err)
			}
			if got := predicate(record); got != tc.want {
				t.Fatalf("match = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSearchGeneratedMessageProvenance(t *testing.T) {
	cases := []struct {
		input     string
		synthetic bool
		text      string
		want      bool
	}{
		{`{"secretKey":"value"}`, true, "secretKey", false},
		{`{"message":{"secretKey":"value"}}`, true, "secretKey", false},
		{`{"message":{"secretKey":"value"}}`, true, "value", true},
		{`{"message":"{\"secretKey\":\"value\"}"}`, false, "secretKey", true},
		{`{"__REALTIME_TIMESTAMP":"1000000","__CURSOR":"cursor","secretKey":"value"}`, true, "secretKey", false},
		{`{"__REALTIME_TIMESTAMP":"1000000","__CURSOR":"cursor","MESSAGE":[104,105]}`, false, "hi", true},
	}
	for _, tc := range cases {
		t.Run(tc.input+tc.text, func(t *testing.T) {
			result, err := parse.NewEngine(parse.Options{}).Load(strings.NewReader(tc.input))
			if err != nil {
				t.Fatal(err)
			}
			record := result.Parsed.Records[0].Entry
			if record.MessageIsJSON != tc.synthetic {
				t.Fatalf("synthetic = %v", record.MessageIsJSON)
			}
			predicate, err := compileSearch(&SearchSpec{Text: tc.text})
			if err != nil {
				t.Fatal(err)
			}
			if predicate(record) != tc.want {
				t.Fatalf("unexpected match for %#v", record)
			}
			encoded, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "MessageIsJSON") {
				t.Fatal("internal provenance leaked")
			}
		})
	}
}

func TestSearchRejectsAllInvalidLinesBeforeCreatingQuery(t *testing.T) {
	service := NewMemoryService(nil)
	_, err := service.Create(context.Background(), CreateRequest{Search: &SearchSpec{Text: "ok\r\n\r\n[\n(?=x)\n(x)\\1", Mode: "regexp"}})
	apiErr := AsAPIError(err)
	if apiErr.Code != "invalid_search" {
		t.Fatalf("error = %v", err)
	}
	var lines []int
	for _, issue := range apiErr.LineErrors {
		lines = append(lines, issue.Line)
		if issue.Message == "" {
			t.Fatal("missing message")
		}
	}
	if !reflect.DeepEqual(lines, []int{3, 4, 5}) {
		t.Fatalf("lines = %v", lines)
	}
	if len(service.queries) != 0 {
		t.Fatal("invalid search created a query")
	}
	for _, spec := range []SearchSpec{{Mode: "unknown"}, {Operator: "unknown"}} {
		if _, err := compileSearch(&spec); err == nil {
			t.Fatal("invalid option accepted")
		}
	}
}

func TestSearchCompositionShortCircuitsInOrder(t *testing.T) {
	var calls []string
	filter := func(name string, result bool) Predicate {
		return func(Record) bool { calls = append(calls, name); return result }
	}
	predicate := allPredicates(filter("permanent", false), filter("search", true))
	if predicate(Record{}) || !reflect.DeepEqual(calls, []string{"permanent"}) {
		t.Fatalf("calls = %v", calls)
	}
	calls = nil
	predicate = allPredicates(filter("permanent", true), filter("search", false))
	if predicate(Record{}) || !reflect.DeepEqual(calls, []string{"permanent", "search"}) {
		t.Fatalf("calls = %v", calls)
	}
}

func TestSearchFiltersBeforePaginationAndRetainsSnapshots(t *testing.T) {
	service := NewMemoryService(CompilerFunc(func(string) (Predicate, error) { return func(r Record) bool { return r.Severity == "error" }, nil }))
	service.Append([]Record{{Severity: "error", Message: "alpha"}, {Severity: "info", Message: "alpha beta"}, {Severity: "error", Message: "alpha", Fields: map[string]any{"hidden": "beta"}}, {Severity: "error", Message: "beta"}})
	spec := &SearchSpec{Text: "alpha\nbeta", Operator: "and"}
	created, err := service.Create(context.Background(), CreateRequest{Filter: "permanent", Search: spec})
	if err != nil {
		t.Fatal(err)
	}
	spec.Text = "changed after compilation"
	first := readyState(t, service, created.QueryID).Snapshot
	if first.MatchedCount != "1" || first.ProcessedThrough != "4" {
		t.Fatalf("snapshot = %#v", first)
	}
	service.Append([]Record{{Severity: "error", Message: "ALPHA BETA"}, {Severity: "error", Message: "alpha"}})
	latest, err := service.Get(context.Background(), created.QueryID)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Snapshot.MatchedCount != "2" || latest.Snapshot.ProcessedThrough != "6" {
		t.Fatalf("snapshot = %#v", latest.Snapshot)
	}
	for _, tc := range []struct {
		token  string
		offset uint64
		want   string
	}{{first.SnapshotToken, 0, "3"}, {latest.Snapshot.SnapshotToken, 1, "5"}} {
		page, err := service.Page(context.Background(), created.QueryID, tc.token, tc.offset, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Rows) != 1 || page.Rows[0].ID != tc.want {
			t.Fatalf("page = %#v", page)
		}
	}
	cleared, err := service.Create(context.Background(), CreateRequest{Filter: "permanent", Search: &SearchSpec{}})
	if err != nil {
		t.Fatal(err)
	}
	if readyState(t, service, cleared.QueryID).Snapshot.MatchedCount != "5" {
		t.Fatal("clearing search removed permanent filtering")
	}
}

func TestSearchAppliesDuringInitialScanCatchUp(t *testing.T) {
	compiler := blockingCompiler{entered: make(chan struct{}, 1), release: make(chan struct{})}
	service := NewMemoryService(compiler)
	service.Append([]Record{{Message: "match"}})
	created, err := service.Create(context.Background(), CreateRequest{Search: &SearchSpec{Text: "match"}})
	if err != nil {
		t.Fatal(err)
	}
	<-compiler.entered
	service.Append([]Record{{Message: "miss"}, {Message: "MATCH"}})
	close(compiler.release)
	snapshot := readyState(t, service, created.QueryID).Snapshot
	if snapshot.MatchedCount != "2" || snapshot.ProcessedThrough != "3" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}
