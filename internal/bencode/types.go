package bencode

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Value is a bencode value. The four bencode types per BEP 3 are string,
// integer, list, and dictionary.
type Value interface {
	Kind() Kind
	fmt.Stringer
}

// Kind identifies the bencode type of a Value.
type Kind int

const (
	KindString Kind = iota
	KindInt
	KindList
	KindDict
)

func (k Kind) String() string {
	switch k {
	case KindString:
		return "string"
	case KindInt:
		return "integer"
	case KindList:
		return "list"
	case KindDict:
		return "dict"
	default:
		return "Kind(" + strconv.Itoa(int(k)) + ")"
	}
}

// StringVal is a bencoded byte string.
type StringVal struct {
	S string
}

func (v StringVal) Kind() Kind { return KindString }

func (v StringVal) String() string {
	n := len(v.S)
	// Short strings: use stack allocation for the encoded form.
	if n < 10 {
		buf := make([]byte, 1+1+n)
		buf[0] = '0' + byte(n)
		buf[1] = ':'
		copy(buf[2:], v.S)
		return string(buf)
	}
	var b strings.Builder
	b.Grow(1 + n + 20)
	b.WriteString(strconv.Itoa(n))
	b.WriteByte(':')
	b.WriteString(v.S)
	return b.String()
}

// IntVal is a bencoded integer.
type IntVal struct {
	I int64
}

func (v IntVal) Kind() Kind { return KindInt }

func (v IntVal) String() string {
	var b strings.Builder
	b.Grow(24)
	b.WriteByte('i')
	b.WriteString(strconv.FormatInt(v.I, 10))
	b.WriteByte('e')
	return b.String()
}

// ListVal is a bencoded list.
type ListVal struct {
	L []Value
}

func (v ListVal) Kind() Kind { return KindList }

func (v ListVal) String() string {
	var b strings.Builder
	b.Grow(64)
	b.WriteByte('l')
	for _, elem := range v.L {
		b.WriteString(elem.String())
	}
	b.WriteByte('e')
	return b.String()
}

// DictVal is a bencoded dictionary. Keys maintains insertion order for
// round-trip stability. The encoder re-sorts keys as needed for output.
type DictVal struct {
	Keys   []string
	Values map[string]Value
}

func (v *DictVal) Kind() Kind { return KindDict }

func (v *DictVal) String() string {
	sorted := make([]string, len(v.Keys))
	copy(sorted, v.Keys)
	sort.Strings(sorted)

	var b strings.Builder
	b.Grow(128)
	b.WriteByte('d')
	for _, k := range sorted {
		n := len(k)
		b.WriteString(strconv.Itoa(n))
		b.WriteByte(':')
		b.WriteString(k)
		b.WriteString(v.Values[k].String())
	}
	b.WriteByte('e')
	return b.String()
}

// Set adds or replaces a key-value pair in the dictionary. If key already
// exists the value is updated; if not the key is appended to Keys.
func (d *DictVal) Set(key string, val Value) {
	if _, exists := d.Values[key]; !exists {
		d.Keys = append(d.Keys, key)
	}
	d.Values[key] = val
}

// Get returns the value for key. The bool result indicates whether the key
// was present.
func (d *DictVal) Get(key string) (Value, bool) {
	v, ok := d.Values[key]
	return v, ok
}

// Reset clears the dictionary for reuse. The underlying map and slice
// are retained for future use.
func (d *DictVal) Reset() {
	for k := range d.Values {
		delete(d.Values, k)
	}
	d.Keys = d.Keys[:0]
}

// DictPool is a sync.Pool for DictVal to reduce allocations in hot paths.
var DictPool = sync.Pool{
	New: func() any {
		return &DictVal{
			Keys:   make([]string, 0, 8),
			Values: make(map[string]Value, 8),
		}
	},
}

// AcquireDict gets a DictVal from the pool. The result must be released
// with ReleaseDict when no longer needed.
func AcquireDict() *DictVal {
	return DictPool.Get().(*DictVal)
}

// ReleaseDict returns a DictVal to the pool after resetting it.
func ReleaseDict(d *DictVal) {
	if d == nil {
		return
	}
	d.Reset()
	DictPool.Put(d)
}

// NewString returns a StringVal wrapping s.
func NewString(s string) Value { return StringVal{S: s} }

// NewInt returns an IntVal wrapping i.
func NewInt(i int64) Value { return IntVal{I: i} }

// NewList returns a ListVal containing items.
func NewList(items ...Value) Value { return ListVal{L: items} }

// NewDict returns an empty, initialized DictVal.
func NewDict() *DictVal { return &DictVal{Keys: []string{}, Values: make(map[string]Value)} }
