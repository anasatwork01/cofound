package httpx

import "encoding/json"

// jsonMarshal exists so stream.go does not import encoding/json directly,
// keeping one place to change if a faster encoder is ever adopted.
func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
