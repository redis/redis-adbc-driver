// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.

package redis

import "github.com/apache/arrow-adbc/go/adbc"

// parseSearchKeys reads FT.SEARCH NOCONTENT without interpreting document IDs
// as text. The total is the match count, not necessarily this bounded page size.
func parseSearchKeys(reply any) ([]string, error) {
	count, err := parseSearchCount(reply)
	if err != nil {
		return nil, err
	}
	var results []any
	resp2, isRESP2 := reply.([]any)
	if isRESP2 {
		results = resp2[1:]
	} else {
		value, _ := redisMapField(reply, "results")
		var ok bool
		results, ok = value.([]any)
		if !ok {
			return nil, errorf(adbc.StatusInternal, "unexpected FT.SEARCH key results %T", value)
		}
	}
	if count < int64(len(results)) {
		return nil, errorf(adbc.StatusInternal, "inconsistent FT.SEARCH key count")
	}
	keys := make([]string, len(results))
	for i, value := range results {
		if !isRESP2 {
			value, _ = redisMapField(value, "id")
		}
		switch key := value.(type) {
		case string:
			keys[i] = key
		case []byte:
			keys[i] = string(key)
		default:
			return nil, errorf(adbc.StatusInternal, "unexpected FT.SEARCH document ID %T", value)
		}
	}
	return keys, nil
}
