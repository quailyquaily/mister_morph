package entryutil

import (
	"reflect"
	"testing"
)

func TestParseMetadataTuples(t *testing.T) {
	got, ok := ParseMetadataTuples("[Created](2026-08-18 10:30), [Ref](tg:123)")
	if !ok {
		t.Fatal("ParseMetadataTuples() ok = false")
	}
	want := map[string]string{
		"Created": "2026-08-18 10:30",
		"Ref":     "tg:123",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseMetadataTuples() = %#v, want %#v", got, want)
	}

	if _, ok := ParseMetadataTuples("[Ref](one), [Ref](two)"); ok {
		t.Fatal("ParseMetadataTuples() accepted duplicate keys")
	}
}
