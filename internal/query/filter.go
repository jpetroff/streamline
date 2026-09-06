package query

import (
	"bytes"
	"encoding/json"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// FilterSpec is one ordered condition on an original JSON field.
type FilterSpec struct {
	Field  string `json:"field"`
	Op     string `json:"op"`
	Value  any    `json:"value"`
	issues []FilterError
}

// FilterList rejects non-array JSON while allowing omitted filters to default to empty.
type FilterList []FilterSpec

func (filters *FilterList) UnmarshalJSON(data []byte) error {
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("[")) {
		return &APIError{Code: "invalid_filter", Message: "Filters must be a JSON array.", FilterErrors: []FilterError{{Property: "", Message: "Filters must be a JSON array."}}}
	}
	var specs []FilterSpec
	if err := json.Unmarshal(data, &specs); err != nil {
		return err
	}
	*filters = specs
	return nil
}

// FilterError identifies a property of a one-based filter position.
type FilterError struct {
	Index    int    `json:"index"`
	Property string `json:"property"`
	Message  string `json:"message"`
}

// UnmarshalJSON retains shape errors for indexed validation by the compiler.
func (f *FilterSpec) UnmarshalJSON(data []byte) error {
	*f = FilterSpec{}
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil || object == nil {
		f.issues = append(f.issues, FilterError{Message: "Each filter must be an object."})
		return nil
	}
	for key := range object {
		if key != "field" && key != "op" && key != "value" {
			f.issues = append(f.issues, FilterError{Property: key, Message: "Unknown filter property."})
		}
	}
	f.Field, _ = object["field"].(string)
	f.Op, _ = object["op"].(string)
	f.Value = object["value"]
	return nil
}

type TupleCompiler struct{}

// Compile validates all conditions before constructing immutable predicates.
func (TupleCompiler) Compile(specs []FilterSpec) (Predicate, error) {
	var predicates []Predicate
	var issues []FilterError
	for i, spec := range specs {
		report := func(property, message string) {
			issues = append(issues, FilterError{Index: i + 1, Property: property, Message: message})
		}
		for _, issue := range spec.issues {
			report(issue.Property, issue.Message)
		}
		path := strings.Split(spec.Field, ".")
		if strings.TrimSpace(spec.Field) == "" || strings.Contains(spec.Field, "..") || strings.HasPrefix(spec.Field, ".") || strings.HasSuffix(spec.Field, ".") {
			report("field", "Enter a nonempty dotted object path.")
		}
		var match func(any) bool
		switch spec.Op {
		case "eq", "contains", "regex":
			value, ok := spec.Value.(string)
			if !ok {
				report("value", "Text operators require a string value.")
				continue
			}
			var matcher *regexp.Regexp
			var err error
			if spec.Op == "eq" {
				matcher, err = compileLine(`\A(?:`+regexp.QuoteMeta(value)+`)\z`, "regexp")
			} else if spec.Op == "contains" {
				matcher, err = compileLine(value, "plain")
			} else {
				matcher, err = compileLine(value, "regexp")
			}
			if err != nil {
				report("value", err.Error())
				continue
			}
			match = func(value any) bool { text, ok := filterText(value); return ok && matcher.MatchString(text) }
		case "gt", "gte", "lt", "lte":
			threshold, ok := filterNumber(spec.Value)
			if !ok {
				report("value", "Numeric operators require a finite number value.")
				continue
			}
			op := spec.Op
			match = func(value any) bool {
				number, ok := filterNumber(value)
				if !ok {
					return false
				}
				switch op {
				case "gt":
					return number > threshold
				case "gte":
					return number >= threshold
				case "lt":
					return number < threshold
				default:
					return number <= threshold
				}
			}
		default:
			report("op", "Choose eq, contains, regex, gt, gte, lt, or lte.")
			continue
		}
		predicates = append(predicates, func(record Record) bool {
			value, exists := filterField(record.Fields, path)
			return exists && match(value)
		})
	}
	if len(issues) > 0 {
		return nil, &APIError{Code: "invalid_filter", Message: "Check the marked filters.", FilterErrors: issues}
	}
	return allPredicates(predicates...), nil
}

func filterField(fields map[string]any, path []string) (any, bool) {
	var value any = fields
	for _, segment := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok = object[segment]
		if !ok {
			return nil, false
		}
	}
	return value, true
}

func filterNumber(value any) (float64, bool) {
	var number float64
	switch value := value.(type) {
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return 0, false
		}
		number = parsed
	case float64:
		number = value
	default:
		return 0, false
	}
	return number, !math.IsNaN(number) && !math.IsInf(number, 0)
}

func filterText(value any) (string, bool) {
	switch value := value.(type) {
	case string:
		return value, true
	case json.Number:
		return value.String(), true
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64), true
	case bool:
		return strconv.FormatBool(value), true
	case nil:
		return "null", true
	default:
		return "", false
	}
}
