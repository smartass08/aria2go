package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFieldsCount(t *testing.T) {
	var o Options
	fields := o.Fields()
	if len(fields) < 195 {
		t.Errorf("Fields() returned %d entries, expected >= 195", len(fields))
	}
}

func TestFieldsUnique(t *testing.T) {
	var o Options
	fields := o.Fields()
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		if seen[f] {
			t.Errorf("duplicate field %q in Fields()", f)
		}
		seen[f] = true
	}
}

func TestFieldsCoverAllStructFields(t *testing.T) {
	var o Options
	fields := o.Fields()
	fieldSet := make(map[string]bool, len(fields))
	for _, f := range fields {
		fieldSet[f] = true
	}

	tp := reflect.TypeOf(o)
	for i := 0; i < tp.NumField(); i++ {
		sf := tp.Field(i)
		tag := sf.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		if !fieldSet[tag] {
			t.Errorf("struct field %s (json:%q) not found in Fields()", sf.Name, tag)
		}
	}
}

func TestFieldsNoExtraneous(t *testing.T) {
	var o Options
	fields := o.Fields()

	tp := reflect.TypeOf(o)
	structFields := make(map[string]bool, tp.NumField())
	for i := 0; i < tp.NumField(); i++ {
		tag := tp.Field(i).Tag.Get("json")
		if tag != "" && tag != "-" {
			structFields[tag] = true
		}
	}

	for _, f := range fields {
		if !structFields[f] {
			t.Errorf("Fields() entry %q has no corresponding struct field", f)
		}
	}
}

func TestZeroValueDefaults(t *testing.T) {
	var o Options

	if o.Split != 0 {
		t.Errorf("zero-value Split = %d, want 0", o.Split)
	}
	if o.MaxConnectionPerServer != 0 {
		t.Errorf("zero-value MaxConnectionPerServer = %d, want 0", o.MaxConnectionPerServer)
	}
	if o.MaxConcurrentDownloads != 0 {
		t.Errorf("zero-value MaxConcurrentDownloads = %d, want 0", o.MaxConcurrentDownloads)
	}
	if o.EnableHTTPKeepAlive != false {
		t.Errorf("zero-value EnableHTTPKeepAlive = true, want false")
	}
	if o.CheckCertificate != false {
		t.Errorf("zero-value CheckCertificate = true, want false")
	}
	if o.FTPPasv != false {
		t.Errorf("zero-value FTPPasv = true, want false")
	}
	if o.EnableDHT != false {
		t.Errorf("zero-value EnableDHT = true, want false")
	}
	if o.MaxDownloadResult != 0 {
		t.Errorf("zero-value MaxDownloadResult = %d, want 0", o.MaxDownloadResult)
	}
}

func TestJSONRoundTripBasic(t *testing.T) {
	in := Options{
		Dir:                    "/downloads",
		MaxConcurrentDownloads: 10,
		Split:                  8,
		MaxConnectionPerServer: 4,
	}

	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var out Options
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if out.Dir != in.Dir {
		t.Errorf("Dir = %q, want %q", out.Dir, in.Dir)
	}
	if out.MaxConcurrentDownloads != in.MaxConcurrentDownloads {
		t.Errorf("MaxConcurrentDownloads = %d, want %d", out.MaxConcurrentDownloads, in.MaxConcurrentDownloads)
	}
	if out.Split != in.Split {
		t.Errorf("Split = %d, want %d", out.Split, in.Split)
	}
}

func TestJSONTagsUseHyphens(t *testing.T) {
	var o Options
	tp := reflect.TypeOf(o)
	for i := 0; i < tp.NumField(); i++ {
		sf := tp.Field(i)
		tag := sf.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		if containsUnderscore(tag) {
			t.Errorf("field %s uses underscore in json tag %q — aria2 uses hyphens", sf.Name, tag)
		}
	}
}

func containsUnderscore(s string) bool {
	for _, c := range s {
		if c == '_' {
			return true
		}
	}
	return false
}

func TestAccumulativeFieldTypes(t *testing.T) {
	var o Options
	tp := reflect.TypeOf(o)

	accumulative := map[string]bool{
		"header":             true,
		"index-out":          true,
		"bt-tracker":         true,
		"bt-exclude-tracker": true,
		"dht-entry-point":    true,
		"dht-entry-point6":   true,
	}

	for i := 0; i < tp.NumField(); i++ {
		sf := tp.Field(i)
		tag := sf.Tag.Get("json")
		if accumulative[tag] {
			if sf.Type.Kind() != reflect.Slice {
				t.Errorf("accumulative field %s (json:%q) has type %v, want slice", sf.Name, tag, sf.Type)
			}
		}
	}
}

func TestStructFieldCount(t *testing.T) {
	var o Options
	tp := reflect.TypeOf(o)
	count := tp.NumField()
	if count < 195 {
		t.Errorf("Options has %d fields, expected >= 195", count)
	}
}
