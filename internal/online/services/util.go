package services

import (
	"encoding/json"
	"fmt"

	"eridian/internal/db"
	"eridian/internal/online"
)

// toInt tries to extract an int from any numeric type in a map.
func toInt(v any) (int, bool) {
	if v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case int32:
		return int(n), true
	case uint64:
		return int(n), true
	case uint32:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return int(i), true
		}
		return 0, false
	default:
		return 0, false
	}
}

// toInt64 tries to extract an int64 from any numeric type in a map.
func toInt64(v any) (int64, bool) {
	if v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int:
		return int64(n), true
	case int64:
		return n, true
	case int32:
		return int64(n), true
	case uint64:
		return int64(n), true
	case uint32:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return i, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// toFloat64 tries to extract a float64 from a map value.
func toFloat64(v any) (float64, bool) {
	if v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		if err == nil {
			return f, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// resolveCharID returns the char_id from data or the session.
func resolveCharID(c *online.ClientSession, data map[string]any) int64 {
	if cid, ok := toInt64(data["char_id"]); ok && cid != 0 {
		return cid
	}
	return c.CharID
}

// verifyCharOwnership checks that the session owns the given character.
func verifyCharOwnership(c *online.ClientSession, charID int64) error {
	if charID == 0 {
		return nil // skip check if 0
	}
	char, err := db.GetCharacterByID(charID)
	if err != nil {
		return fmt.Errorf("character_not_found")
	}
	if char.AccountID != c.AccountID {
		return fmt.Errorf("not_your_character")
	}
	return nil
}
