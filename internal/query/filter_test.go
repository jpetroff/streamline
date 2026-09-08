package query

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func TestTupleOperators(t *testing.T) {
	record := Record{Timestamp: "normalized", Fields: map[string]any{
		"name": "Café.*\nSECOND", "empty": "", "number": json.Number("12.5"), "bool": true, "null": nil,
		"timestamp": json.Number("100"), "nested": map[string]any{"name": "ERROR"}, "nested.name": "literal", "array": []any{"ERROR"}, "numericString": "12.5",
	}}
	cases := []struct {
		name, field, op string
		value           any
		want            bool
	}{
		{"equal folded", "nested.name", "eq", "error", true},
		{"equal is whole value", "nested.name", "eq", "err", false},
		{"literal equal", "name", "eq", "café.*\nsecond", true},
		{"literal contains", "name", "contains", ".*", true},
		{"unicode folding", "name", "contains", "CAFÉ", true},
		{"regex", "name", "regex", "(?s)^café.*second$", true},
		{"regex newline is one expression", "name", "regex", "café.*\nsecond", true},
		{"regex flag override", "nested.name", "regex", "(?-i:error)", false},
		{"empty string", "empty", "eq", "", true},
		{"empty pattern", "empty", "regex", "", true},
		{"numeric text", "number", "eq", "12.5", true},
		{"boolean text", "bool", "contains", "RU", true},
		{"explicit null", "null", "eq", "NULL", true},
		{"missing", "missing", "eq", "null", false},
		{"case sensitive path", "Nested.name", "eq", "error", false},
		{"container excluded", "nested", "contains", "ERROR", false},
		{"array excluded", "array", "contains", "ERROR", false},
		{"no array indexing", "array.0", "eq", "ERROR", false},
		{"greater", "number", "gt", json.Number("12"), true},
		{"greater boundary", "number", "gt", 12.5, false},
		{"greater equal", "number", "gte", 12.5, true},
		{"less", "number", "lt", 13.0, true},
		{"less boundary", "number", "lt", 12.5, false},
		{"less equal", "number", "lte", 12.5, true},
		{"original numeric timestamp", "timestamp", "gte", 100.0, true},
		{"string is not a number", "numericString", "gt", 0.0, false},
		{"null is not a number", "null", "gte", 0.0, false},
		{"bool is not a number", "bool", "gt", 0.0, false},
		{"missing numeric", "missing", "lt", 10.0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			predicate, err := (TupleCompiler{}).Compile([]FilterSpec{{Field: tc.field, Op: tc.op, Value: tc.value}})
			if err != nil {
				t.Fatal(err)
			}
			if got := predicate(record); got != tc.want {
				t.Fatalf("match = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNegativeTupleOperators(t *testing.T) {
	cases := []struct {
		op                string
		operand           any
		matches, excludes []any
	}{
		{"neq", "error", []any{"err", "", nil, true, json.Number("12.5")}, []any{"ERROR", "error"}},
		{"neq", "café.*\nsecond", []any{"caféZZ\nsecond"}, []any{"CAFÉ.*\nSECOND"}},
		{"neq", "null", []any{"", false}, []any{nil, "NULL"}},
		{"neq", "12.5", []any{json.Number("12"), false}, []any{json.Number("12.5"), 12.5}},
		{"not_contains", "CAFÉ.*", []any{"café", "", nil}, []any{"A café.*\nSECOND"}},
		{"not_contains", "", nil, []any{"", "anything", nil}},
		{"not_regex", "(?s)^café.*second$", []any{"other", "", nil}, []any{"CAFÉ.*\nSECOND"}},
		{"not_regex", "café.*\nsecond", []any{"other"}, []any{"CAFÉ.*\nSECOND"}},
		{"not_regex", "(?-i:error)", []any{"ERROR"}, []any{"error"}},
		{"not_regex", "", nil, []any{"", "anything", nil}},
		{"not_gt", 12.5, []any{12.0, json.Number("12.5")}, []any{13.0}},
		{"not_gte", 12.5, []any{12.0}, []any{json.Number("12.5"), 13.0}},
		{"not_lt", 12.5, []any{json.Number("12.5"), 13.0}, []any{12.0}},
		{"not_lte", 12.5, []any{13.0}, []any{12.0, json.Number("12.5")}},
	}
	for _, tc := range cases {
		t.Run(tc.op, func(t *testing.T) {
			predicate, err := (TupleCompiler{}).Compile([]FilterSpec{{Field: "nested.value", Op: tc.op, Value: tc.operand}})
			if err != nil {
				t.Fatal(err)
			}
			check := func(value any, want bool) {
				t.Helper()
				if got := predicate(Record{Fields: map[string]any{"nested": map[string]any{"value": value}}}); got != want {
					t.Fatalf("operand %#v, source %#v: match = %v, want %v", tc.operand, value, got, want)
				}
			}
			for _, value := range tc.matches {
				check(value, true)
			}
			for _, value := range tc.excludes {
				check(value, false)
			}
			for _, value := range []any{map[string]any{}, []any{"other"}} {
				check(value, false)
			}
			if predicate(Record{}) || predicate(Record{Fields: map[string]any{"nested": map[string]any{}}}) || predicate(Record{Fields: map[string]any{"nested": nil}}) {
				t.Fatal("missing or untraversable field matched")
			}
			if _, numeric := tc.operand.(float64); numeric {
				for _, value := range []any{"12.5", nil, true, math.NaN(), math.Inf(1)} {
					check(value, false)
				}
			}
		})
	}
}

func TestNegativeTupleValidation(t *testing.T) {
	for _, op := range []string{"neq", "not_contains", "not_regex", "not_gt", "not_gte", "not_lt", "not_lte"} {
		for _, value := range []any{nil, true, []any{}, map[string]any{}} {
			if _, err := (TupleCompiler{}).Compile([]FilterSpec{{Field: "value", Op: op, Value: value}}); err == nil {
				t.Fatalf("%s accepted operand %#v", op, value)
			}
		}
	}
	for _, value := range []any{"[", "(?=x)", 10.0} {
		_, err := (TupleCompiler{}).Compile([]FilterSpec{{Field: "value", Op: "not_regex", Value: value}})
		if err == nil {
			t.Fatalf("accepted regex operand %#v", value)
		}
		issues := AsAPIError(err).FilterErrors
		if len(issues) != 1 || issues[0].Index != 1 || issues[0].Property != "value" {
			t.Fatalf("issues = %#v", issues)
		}
	}
}

func TestTupleValidationReportsEveryFilter(t *testing.T) {
	var specs []FilterSpec
	err := json.Unmarshal([]byte(`[null,[],{"field":"","op":"eq","value":false},{"field":"a..b","op":"bad","value":"x"},{"field":"n","op":"gt","value":"10"},{"field":"s","op":"regex","value":"["},{"field":"s","op":"regex","value":"(?=x)"},{"field":"s","op":"eq","value":"x","extra":true},{"field":"s","op":"eq"}]`), &specs)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (TupleCompiler{}).Compile(specs)
	apiErr := AsAPIError(err)
	if apiErr.Code != "invalid_filter" {
		t.Fatalf("error = %#v", apiErr)
	}
	indexes := map[int]bool{}
	for _, issue := range apiErr.FilterErrors {
		indexes[issue.Index] = true
		if issue.Message == "" {
			t.Fatal("missing message")
		}
	}
	if len(indexes) != len(specs) {
		t.Fatalf("missing indexed errors: %#v", apiErr.FilterErrors)
	}
	for _, value := range []any{nil, true, "1", math.NaN(), math.Inf(1)} {
		if _, err := (TupleCompiler{}).Compile([]FilterSpec{{Field: "n", Op: "gt", Value: value}}); err == nil {
			t.Fatalf("accepted numeric operand %#v", value)
		}
	}
}

func TestTuplePredicatesAreImmutableAndComposeInOrder(t *testing.T) {
	specs := []FilterSpec{{Field: "level", Op: "eq", Value: "error"}, {Field: "n", Op: "gt", Value: 1.0}, {Field: "level", Op: "eq", Value: "error"}}
	predicate, err := (TupleCompiler{}).Compile(specs)
	if err != nil {
		t.Fatal(err)
	}
	specs[0].Field = "other"
	specs[1].Value = 1000.0
	if !predicate(Record{Fields: map[string]any{"level": "ERROR", "n": json.Number("2")}}) {
		t.Fatal("caller mutation changed compiled filters")
	}
	if predicate(Record{Fields: map[string]any{"level": "error", "n": json.Number("0")}}) {
		t.Fatal("filters were not ANDed")
	}
	empty, err := (TupleCompiler{}).Compile(nil)
	if err != nil || !empty(Record{}) {
		t.Fatal("empty filters must match")
	}
	var calls []int
	ordered := allPredicates(func(Record) bool { calls = append(calls, 1); return true }, func(Record) bool { calls = append(calls, 2); return false }, func(Record) bool { calls = append(calls, 3); return true })
	if ordered(Record{}) || !reflect.DeepEqual(calls, []int{1, 2}) {
		t.Fatalf("evaluation order = %v", calls)
	}
}

func TestTupleFiltersSearchPaginationAndStreaming(t *testing.T) {
	service := NewMemoryService(nil)
	row := func(level, duration, message string) Record {
		return Record{Message: message, Fields: map[string]any{"level": level, "duration": json.Number(duration)}}
	}
	service.Append([]Record{row("error", "50", "timeout"), row("info", "200", "timeout"), row("error", "200", "ok"), row("ERROR", "200", "timeout")})
	filters := []FilterSpec{{Field: "level", Op: "neq", Value: "info"}, {Field: "level", Op: "not_contains", Value: "debug"}, {Field: "level", Op: "not_regex", Value: "^warn"}, {Field: "duration", Op: "not_lt", Value: 100.0}}
	created, err := service.Create(context.Background(), CreateRequest{Filter: filters, Search: &SearchSpec{Text: "timeout"}})
	if err != nil {
		t.Fatal(err)
	}
	first := readyState(t, service, created.QueryID).Snapshot
	if first.MatchedCount != "1" {
		t.Fatalf("first count = %s", first.MatchedCount)
	}
	service.Append([]Record{row("error", "300", "TIMEOUT"), row("info", "300", "timeout")})
	latest, _ := service.Get(context.Background(), created.QueryID)
	if latest.Snapshot.MatchedCount != "2" {
		t.Fatalf("live count = %s", latest.Snapshot.MatchedCount)
	}
	for _, tc := range []struct {
		token  string
		offset uint64
		id     string
	}{{first.SnapshotToken, 0, "4"}, {latest.Snapshot.SnapshotToken, 1, "5"}} {
		page, err := service.Page(context.Background(), created.QueryID, tc.token, tc.offset, 1)
		if err != nil || len(page.Rows) != 1 || page.Rows[0].ID != tc.id {
			t.Fatalf("filtered page = %#v, %v", page, err)
		}
	}
	clearedSearch, _ := service.Create(context.Background(), CreateRequest{Filter: filters})
	if readyState(t, service, clearedSearch.QueryID).Snapshot.MatchedCount != "3" {
		t.Fatal("clearing search lost filters")
	}
	clearedFilters, _ := service.Create(context.Background(), CreateRequest{Search: &SearchSpec{Text: "timeout"}})
	if readyState(t, service, clearedFilters.QueryID).Snapshot.MatchedCount != "5" {
		t.Fatal("clearing filters lost search")
	}
}

func TestTupleFiltersApplyToInitialScanCatchUp(t *testing.T) {
	entered, release := make(chan struct{}, 1), make(chan struct{})
	service := NewMemoryService(CompilerFunc(func(filters []FilterSpec) (Predicate, error) {
		match, err := (TupleCompiler{}).Compile(filters)
		return func(record Record) bool {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-release
			return match(record)
		}, err
	}))
	service.Append([]Record{{Fields: map[string]any{"n": json.Number("1")}}})
	created, err := service.Create(context.Background(), CreateRequest{Filter: []FilterSpec{{Field: "n", Op: "gt", Value: 1.0}}})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	service.Append([]Record{{Fields: map[string]any{"n": json.Number("2")}}, {Fields: map[string]any{"n": "3"}}})
	close(release)
	ready := readyState(t, service, created.QueryID)
	if ready.Snapshot.MatchedCount != "1" || ready.Snapshot.ProcessedThrough != "3" {
		t.Fatalf("catch-up snapshot = %#v", ready.Snapshot)
	}
}

func TestFilterErrorSnapshotsOwnDiagnostics(t *testing.T) {
	state := State{Error: &APIError{Code: "invalid_filter", FilterErrors: []FilterError{{Index: 1, Property: "value", Message: "Invalid regex"}}}}
	copied := cloneState(state)
	copied.Error.FilterErrors[0].Message = "changed"
	if state.Error.FilterErrors[0].Message != "Invalid regex" {
		t.Fatal("snapshot mutation changed original diagnostics")
	}
}
