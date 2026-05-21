package config

import (
	"reflect"
	"strings"
)

// Merge layers options in precedence order (last wins).
// Typical usage: Merge(defaults, conf, env, args)
// For scalar fields: last non-zero value wins (non-empty string, non-zero int, true bool).
// Values parsed from CLI/config are tracked as explicit, so false, 0, and empty
// string can still override lower-precedence layers.
// For slice fields: accumulative slices (header, bt-tracker, bt-exclude-tracker,
// index-out, dht-entry-point, dht-entry-point6) are concatenated;
// non-accumulative slice fields are replaced.
// Returns a newly allocated Options.
func Merge(layers ...*Options) *Options {
	result := &Options{}
	for _, layer := range layers {
		if layer == nil {
			continue
		}
		mergeInto(result, layer)
	}
	return result
}

func mergeInto(dst, src *Options) {
	// Use fieldMergers for all registered keys.
	for _, merger := range fieldMergers {
		merger(dst, src)
	}
	applyExplicitScalarOverrides(dst, src)
}

func applyExplicitScalarOverrides(dst, src *Options) {
	if len(src.explicit) == 0 {
		return
	}
	dstValue := reflect.ValueOf(dst).Elem()
	srcValue := reflect.ValueOf(src).Elem()
	for name := range src.explicit {
		fieldIndex, ok := optionFieldIndices[name]
		if !ok {
			continue
		}
		srcField := srcValue.Field(fieldIndex)
		if srcField.Kind() == reflect.Slice {
			continue
		}
		dstValue.Field(fieldIndex).Set(srcField)
		dst.markExplicit(name)
	}
}

var optionFieldIndices = func() map[string]int {
	typ := reflect.TypeOf(Options{})
	fields := make(map[string]int, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		key := typ.Field(i).Tag.Get("json")
		if key == "" || key == "-" {
			continue
		}
		if idx := strings.IndexByte(key, ','); idx >= 0 {
			key = key[:idx]
		}
		if key != "" && key != "-" {
			fields[key] = i
		}
	}
	return fields
}()
