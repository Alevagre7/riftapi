package domain

import "encoding/json"

// Index is the response shape for /index/* endpoints (e.g. /index/card-names).
// `Type` identifies the index ("card-names", "rarities", "domains", ...);
// `Values` is the sorted unique list of values; `Total` is len(Values).
type Index struct {
	Total  int          `json:"total"`
	Type   string       `json:"type"`
	Values []IndexValue `json:"values"`
}

// IndexValue is one entry in an Index's Values list. The riftcodex wire
// format permits either a string or an integer, so we carry a separate
// pointer for each kind and pick the right one in MarshalJSON. Exactly
// one of StringValue / IntValue is set on a valid value.
type IndexValue struct {
	StringValue *string
	IntValue    *int
}

// StringIndexValue is a small constructor that keeps call sites readable.
func StringIndexValue(s string) IndexValue {
	return IndexValue{StringValue: &s}
}

// IntIndexValue is a small constructor that keeps call sites readable.
func IntIndexValue(n int) IndexValue {
	return IndexValue{IntValue: &n}
}

// MarshalJSON emits the value as a string, an integer, or null, matching
// the riftcodex wire format. Exactly one of StringValue / IntValue is
// expected to be set; if neither is, the output is null.
func (v IndexValue) MarshalJSON() ([]byte, error) {
	switch {
	case v.StringValue != nil:
		return json.Marshal(*v.StringValue)
	case v.IntValue != nil:
		return json.Marshal(*v.IntValue)
	default:
		return []byte("null"), nil
	}
}
