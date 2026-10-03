// Package sonic is a drop-in replacement for github.com/bytedance/sonic that
// delegates to encoding/json.
//
// Why it exists: several dependencies (eino, eino-ext, go-openai) import
// bytedance/sonic, whose real implementation deliberately refuses to compile
// on 32-bit platforms (android-armv7). Since Go 1.27, upstream sonic itself
// runs in UseStdJSON mode on every platform, so this shim is behaviorally
// identical while keeping the 32-bit build green.
//
// It only covers the API surface our dependencies actually use (sonic.Marshal,
// sonic.Unmarshal, sonic.MarshalString, sonic.UnmarshalString,
// sonic.MarshalIndent, sonic.GetFromString); if a dependency starts using more
// of sonic's API, the build fails with a compile error — extend this file
// then.
package sonic

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Marshal returns the JSON encoding of val.
func Marshal(val interface{}) ([]byte, error) {
	return json.Marshal(val)
}

// MarshalIndent is like Marshal but indents the output.
func MarshalIndent(val interface{}, prefix, indent string) ([]byte, error) {
	return json.MarshalIndent(val, prefix, indent)
}

// Unmarshal parses the JSON-encoded data and stores the result in the value
// pointed to by val.
func Unmarshal(data []byte, val interface{}) error {
	return json.Unmarshal(data, val)
}

// MarshalString returns the JSON encoding of val as a string.
func MarshalString(val interface{}) (string, error) {
	b, err := json.Marshal(val)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// UnmarshalString parses the JSON-encoded data and stores the result in the
// value pointed to by val.
func UnmarshalString(data string, val interface{}) error {
	return json.Unmarshal([]byte(data), val)
}

// Node is a minimal stand-in for sonic/ast.Node: a JSON value extracted by
// Get/GetFromString.
type Node struct {
	raw []byte
}

// MarshalJSON implements json.Marshaler, returning the JSON of the node.
func (n *Node) MarshalJSON() ([]byte, error) {
	if n == nil {
		return []byte("null"), nil
	}
	return n.raw, nil
}

// GetFromString parses data as JSON and returns the node found at the given
// path: string elements select object keys, int elements select array
// indexes.
//
// ponytail: unlike upstream ast, the returned node re-encodes the value (map
// key order is not preserved); numbers are kept exact via json.Number. Good
// enough for extracting fields from LLM JSON output; upgrade path is a
// token-based raw-slice extractor if exact bytes are ever needed.
func GetFromString(data string, path ...interface{}) (*Node, error) {
	dec := json.NewDecoder(strings.NewReader(data))
	dec.UseNumber()
	var cur interface{}
	if err := dec.Decode(&cur); err != nil {
		return nil, err
	}
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, ok := cur.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("GetFromString: cannot get key %q of a non-object", k)
			}
			if cur, ok = m[k]; !ok {
				return nil, fmt.Errorf("GetFromString: key %q not found", k)
			}
		case int:
			arr, ok := cur.([]interface{})
			if !ok {
				return nil, fmt.Errorf("GetFromString: cannot index a non-array with %d", k)
			}
			if k < 0 || k >= len(arr) {
				return nil, fmt.Errorf("GetFromString: index %d out of range", k)
			}
			cur = arr[k]
		default:
			return nil, fmt.Errorf("GetFromString: unsupported path element %v (%T)", p, p)
		}
	}
	raw, err := json.Marshal(cur)
	if err != nil {
		return nil, err
	}
	return &Node{raw: raw}, nil
}
