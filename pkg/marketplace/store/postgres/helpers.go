package postgres

import (
	"encoding/json"
	"time"
)

// time helpers.
func timeToMicro(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMicro()
}

func microToTime(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMicro(v).UTC()
}

// json helpers.
func marshalJSON(v any) (string, error) {
	if v == nil {
		return "{}", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshalJSON(data []byte, v any) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, v)
}
