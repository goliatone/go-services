package core

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestToIntValueRejectsUnsignedOverflow(t *testing.T) {
	_, err := toIntValue(uint64(math.MaxInt64) + 1)
	if err == nil || !strings.Contains(err.Error(), "overflows int64") {
		t.Fatalf("expected uint64 overflow error, got %v", err)
	}

	if uint64(^uint(0)) > math.MaxInt64 {
		_, err = toIntValue(^uint(0))
		if err == nil || !strings.Contains(err.Error(), "overflows int64") {
			t.Fatalf("expected uint overflow error, got %v", err)
		}
	}
}

func TestToIntValueConversionMatrix(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  int64
	}{
		{"int", int(1), 1}, {"int8", int8(2), 2}, {"int16", int16(3), 3},
		{"int32", int32(4), 4}, {"int64", int64(5), 5}, {"uint", uint(6), 6},
		{"uint8", uint8(7), 7}, {"uint16", uint16(8), 8}, {"uint32", uint32(9), 9},
		{"uint64", uint64(10), 10}, {"float32", float32(11), 11}, {"float64", 12.0, 12},
		{"bool true", true, 1}, {"bool false", false, 0}, {"json integer", json.Number("13"), 13},
		{"json float", json.Number("14.9"), 14}, {"string", "15", 15},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := toIntValue(test.input)
			if err != nil || got != test.want {
				t.Fatalf("toIntValue(%T(%v)) = %d, %v; want %d", test.input, test.input, got, err, test.want)
			}
		})
	}
	for _, input := range []any{"", "not-an-int", struct{}{}} {
		if _, err := toIntValue(input); err == nil {
			t.Fatalf("expected conversion failure for %T(%v)", input, input)
		}
	}
}

func TestToFloatValueConversionMatrix(t *testing.T) {
	tests := []any{
		int(1), int8(1), int16(1), int32(1), int64(1),
		uint(1), uint8(1), uint16(1), uint32(1), uint64(1),
		float32(1), float64(1), true, json.Number("1"), "1",
	}
	for _, input := range tests {
		got, err := toFloatValue(input)
		if err != nil || got != 1 {
			t.Fatalf("toFloatValue(%T(%v)) = %v, %v; want 1", input, input, got, err)
		}
	}
	for _, input := range []any{"", "not-a-float", struct{}{}} {
		if _, err := toFloatValue(input); err == nil {
			t.Fatalf("expected conversion failure for %T(%v)", input, input)
		}
	}
}

func TestToBoolValueConversionMatrix(t *testing.T) {
	tests := []struct {
		input any
		want  bool
	}{
		{true, true}, {false, false}, {int(1), true}, {int8(0), false}, {int16(1), true},
		{int32(0), false}, {int64(1), true}, {uint(0), false}, {uint8(1), true},
		{uint16(0), false}, {uint32(1), true}, {uint64(0), false}, {float32(1), true},
		{float64(0), false}, {json.Number("1"), true}, {"true", true}, {"0", false},
	}
	for _, test := range tests {
		got, err := toBoolValue(test.input)
		if err != nil || got != test.want {
			t.Fatalf("toBoolValue(%T(%v)) = %v, %v; want %v", test.input, test.input, got, err, test.want)
		}
	}
	for _, input := range []any{"", "not-a-bool", struct{}{}} {
		if _, err := toBoolValue(input); err == nil {
			t.Fatalf("expected conversion failure for %T(%v)", input, input)
		}
	}
}

func TestApplyMappingTransformMatrix(t *testing.T) {
	unix := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).Unix()
	tests := []struct {
		transform string
		input     any
		want      any
	}{
		{"identity", "value", "value"}, {"to_string", 12, "12"}, {"to_int", "12", int64(12)},
		{"to_float", "1.5", 1.5}, {"to_bool", "true", true}, {"trim", " x ", "x"},
		{"lowercase", "AbC", "abc"}, {"uppercase", "AbC", "ABC"},
		{"unix_time_to_rfc3339", unix, "2026-01-02T03:04:05Z"},
	}
	for _, test := range tests {
		got, err := applyMappingTransform(test.transform, test.input)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Fatalf("applyMappingTransform(%q, %v) = %#v, %v; want %#v", test.transform, test.input, got, err, test.want)
		}
	}
	if _, err := applyMappingTransform("unknown", "value"); err == nil {
		t.Fatalf("expected unsupported transform error")
	}
}
