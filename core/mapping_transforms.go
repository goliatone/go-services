package core

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func applyMappingTransform(transform string, value any) (any, error) {
	switch normalizeTransform(transform) {
	case "identity":
		return value, nil
	case "to_string":
		return fmt.Sprint(value), nil
	case "to_int":
		out, err := toIntValue(value)
		if err != nil {
			return nil, err
		}
		return out, nil
	case "to_float":
		out, err := toFloatValue(value)
		if err != nil {
			return nil, err
		}
		return out, nil
	case "to_bool":
		out, err := toBoolValue(value)
		if err != nil {
			return nil, err
		}
		return out, nil
	case "trim":
		out, err := toStringStrict(value)
		if err != nil {
			return nil, err
		}
		return strings.TrimSpace(out), nil
	case "lowercase":
		out, err := toStringStrict(value)
		if err != nil {
			return nil, err
		}
		return strings.ToLower(out), nil
	case "uppercase":
		out, err := toStringStrict(value)
		if err != nil {
			return nil, err
		}
		return strings.ToUpper(out), nil
	case "unix_time_to_rfc3339":
		unixValue, err := toIntValue(value)
		if err != nil {
			return nil, err
		}
		return time.Unix(unixValue, 0).UTC().Format(time.RFC3339), nil
	default:
		return nil, fmt.Errorf("core: unsupported transform %q", transform)
	}
}

func toIntValue(value any) (int64, error) {
	switch typed := value.(type) {
	case int:
		return int64(typed), nil
	case int8:
		return int64(typed), nil
	case int16:
		return int64(typed), nil
	case int32:
		return int64(typed), nil
	case int64:
		return typed, nil
	case uint:
		return int64(typed), nil
	case uint8:
		return int64(typed), nil
	case uint16:
		return int64(typed), nil
	case uint32:
		return int64(typed), nil
	case uint64:
		return int64(typed), nil
	case float32:
		return int64(typed), nil
	case float64:
		return int64(typed), nil
	case bool:
		if typed {
			return 1, nil
		}
		return 0, nil
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil {
			return parsed, nil
		}
		floatParsed, floatErr := typed.Float64()
		if floatErr != nil {
			return 0, fmt.Errorf("core: parse number as int: %w", err)
		}
		return int64(floatParsed), nil
	case string:
		candidate := strings.TrimSpace(typed)
		if candidate == "" {
			return 0, fmt.Errorf("core: empty string cannot convert to int")
		}
		parsed, err := strconv.ParseInt(candidate, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("core: parse string as int: %w", err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("core: unsupported int conversion from %T", value)
	}
}

func toFloatValue(value any) (float64, error) {
	switch typed := value.(type) {
	case int:
		return float64(typed), nil
	case int8:
		return float64(typed), nil
	case int16:
		return float64(typed), nil
	case int32:
		return float64(typed), nil
	case int64:
		return float64(typed), nil
	case uint:
		return float64(typed), nil
	case uint8:
		return float64(typed), nil
	case uint16:
		return float64(typed), nil
	case uint32:
		return float64(typed), nil
	case uint64:
		return float64(typed), nil
	case float32:
		return float64(typed), nil
	case float64:
		return typed, nil
	case bool:
		if typed {
			return 1, nil
		}
		return 0, nil
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, fmt.Errorf("core: parse number as float: %w", err)
		}
		return parsed, nil
	case string:
		candidate := strings.TrimSpace(typed)
		if candidate == "" {
			return 0, fmt.Errorf("core: empty string cannot convert to float")
		}
		parsed, err := strconv.ParseFloat(candidate, 64)
		if err != nil {
			return 0, fmt.Errorf("core: parse string as float: %w", err)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("core: unsupported float conversion from %T", value)
	}
}

func toBoolValue(value any) (bool, error) {
	switch typed := value.(type) {
	case bool:
		return typed, nil
	case int:
		return typed != 0, nil
	case int8:
		return typed != 0, nil
	case int16:
		return typed != 0, nil
	case int32:
		return typed != 0, nil
	case int64:
		return typed != 0, nil
	case uint:
		return typed != 0, nil
	case uint8:
		return typed != 0, nil
	case uint16:
		return typed != 0, nil
	case uint32:
		return typed != 0, nil
	case uint64:
		return typed != 0, nil
	case float32:
		return typed != 0, nil
	case float64:
		return typed != 0, nil
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return false, fmt.Errorf("core: parse number as bool: %w", err)
		}
		return parsed != 0, nil
	case string:
		candidate := strings.TrimSpace(strings.ToLower(typed))
		switch candidate {
		case "true", "1", "yes", "y":
			return true, nil
		case "false", "0", "no", "n":
			return false, nil
		default:
			return false, fmt.Errorf("core: parse string as bool: %q", typed)
		}
	default:
		return false, fmt.Errorf("core: unsupported bool conversion from %T", value)
	}
}

func toStringStrict(value any) (string, error) {
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("core: expected string input for text transform, got %T", value)
	}
	return text, nil
}
