package config

import (
	"testing"
)

// --- BooleanOptionHandler ---

func TestBooleanOptionHandlerTrue(t *testing.T) {
	h := NewBooleanOptionHandler("daemon", false)
	v, err := h.Parse("true")
	if err != nil {
		t.Fatalf("Parse(true): %v", err)
	}
	if v != true {
		t.Errorf("Parse(true) = %v, want true", v)
	}
}

func TestBooleanOptionHandlerFalse(t *testing.T) {
	h := NewBooleanOptionHandler("daemon", false)
	v, err := h.Parse("false")
	if err != nil {
		t.Fatalf("Parse(false): %v", err)
	}
	if v != false {
		t.Errorf("Parse(false) = %v, want false", v)
	}
}

func TestBooleanOptionHandlerInvalid(t *testing.T) {
	h := NewBooleanOptionHandler("daemon", false)
	_, err := h.Parse("hello")
	if err == nil {
		t.Fatal("Parse(hello) returned nil error, want error")
	}
	cfgErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error is %T, want *config.Error", err)
	}
	if cfgErr.Code != ErrInvalidOption {
		t.Errorf("error code = %d, want %d", cfgErr.Code, ErrInvalidOption)
	}
}

func TestBooleanOptionHandlerAllowedValues(t *testing.T) {
	h := NewBooleanOptionHandler("daemon", false)
	allowed := h.AllowedValues()
	if len(allowed) != 2 {
		t.Fatalf("AllowedValues = %v, want [true, false]", allowed)
	}
}

func TestBooleanOptionHandlerName(t *testing.T) {
	h := NewBooleanOptionHandler("daemon", false)
	if h.Name() != "daemon" {
		t.Errorf("Name() = %q, want daemon", h.Name())
	}
}

func TestBooleanOptionHandlerDefaultValue(t *testing.T) {
	h := NewBooleanOptionHandler("daemon", true)
	if h.DefaultValue() != true {
		t.Errorf("DefaultValue() = %v, want true", h.DefaultValue())
	}
	h2 := NewBooleanOptionHandler("daemon", false)
	if h2.DefaultValue() != false {
		t.Errorf("DefaultValue() = %v, want false", h2.DefaultValue())
	}
}

// --- NumberOptionHandler ---

func TestNumberOptionHandler(t *testing.T) {
	h := NewNumberOptionHandler("timeout", 0, -1, -1)
	v, err := h.Parse("0")
	if err != nil {
		t.Fatalf("Parse(0): %v", err)
	}
	if v != int64(0) {
		t.Errorf("Parse(0) = %v, want 0", v)
	}
}

func TestNumberOptionHandlerAllowedValuesUnbounded(t *testing.T) {
	h := NewNumberOptionHandler("timeout", 0, -1, -1)
	allowed := h.AllowedValues()
	if len(allowed) != 1 || allowed[0] != "*-*" {
		t.Errorf("AllowedValues() = %v, want [*-*]", allowed)
	}
}

func TestNumberOptionHandlerMin(t *testing.T) {
	h := NewNumberOptionHandler("timeout", 0, 1, -1)
	v, err := h.Parse("1")
	if err != nil {
		t.Fatalf("Parse(1): %v", err)
	}
	if v != int64(1) {
		t.Errorf("Parse(1) = %v, want 1", v)
	}
	_, err = h.Parse("0")
	if err == nil {
		t.Fatal("Parse(0) with min=1 returned nil, want error")
	}
}

func TestNumberOptionHandlerAllowedValuesMin(t *testing.T) {
	h := NewNumberOptionHandler("timeout", 0, 1, -1)
	allowed := h.AllowedValues()
	if len(allowed) != 1 || allowed[0] != "1-*" {
		t.Errorf("AllowedValues() = %v, want [1-*]", allowed)
	}
}

func TestNumberOptionHandlerMax(t *testing.T) {
	h := NewNumberOptionHandler("timeout", 0, -1, 100)
	v, err := h.Parse("100")
	if err != nil {
		t.Fatalf("Parse(100): %v", err)
	}
	if v != int64(100) {
		t.Errorf("Parse(100) = %v, want 100", v)
	}
	_, err = h.Parse("101")
	if err == nil {
		t.Fatal("Parse(101) with max=100 returned nil, want error")
	}
}

func TestNumberOptionHandlerAllowedValuesMax(t *testing.T) {
	h := NewNumberOptionHandler("timeout", 0, -1, 100)
	allowed := h.AllowedValues()
	if len(allowed) != 1 || allowed[0] != "*-100" {
		t.Errorf("AllowedValues() = %v, want [*-100]", allowed)
	}
}

func TestNumberOptionHandlerMinMax(t *testing.T) {
	h := NewNumberOptionHandler("timeout", 0, 1, 100)
	v, err := h.Parse("1")
	if err != nil {
		t.Fatalf("Parse(1): %v", err)
	}
	if v != int64(1) {
		t.Errorf("Parse(1) = %v, want 1", v)
	}
	v, err = h.Parse("100")
	if err != nil {
		t.Fatalf("Parse(100): %v", err)
	}
	if v != int64(100) {
		t.Errorf("Parse(100) = %v, want 100", v)
	}
	_, err = h.Parse("0")
	if err == nil {
		t.Fatal("Parse(0) with min=1 returned nil, want error")
	}
	_, err = h.Parse("101")
	if err == nil {
		t.Fatal("Parse(101) with max=100 returned nil, want error")
	}
}

func TestNumberOptionHandlerAllowedValuesMinMax(t *testing.T) {
	h := NewNumberOptionHandler("timeout", 0, 1, 100)
	allowed := h.AllowedValues()
	if len(allowed) != 1 || allowed[0] != "1-100" {
		t.Errorf("AllowedValues() = %v, want [1-100]", allowed)
	}
}

func TestNumberOptionHandlerBadInput(t *testing.T) {
	h := NewNumberOptionHandler("timeout", 0, -1, -1)
	_, err := h.Parse("abc")
	if err == nil {
		t.Fatal("Parse(abc) returned nil, want error")
	}
}

func TestNumberOptionHandlerDefault(t *testing.T) {
	h := NewNumberOptionHandler("timeout", 60, -1, -1)
	if h.DefaultValue() != int64(60) {
		t.Errorf("DefaultValue() = %v, want 60", h.DefaultValue())
	}
}

// --- UnitNumberOptionHandler ---

func TestUnitNumberOptionHandler(t *testing.T) {
	h := NewUnitNumberOptionHandler("timeout", "0", "", "")
	v, err := h.Parse("4294967296")
	if err != nil {
		t.Fatalf("Parse(4294967296): %v", err)
	}
	if v != int64(4294967296) {
		t.Errorf("Parse(4294967296) = %v, want 4294967296", v)
	}
}

func TestUnitNumberOptionHandlerM(t *testing.T) {
	h := NewUnitNumberOptionHandler("timeout", "0", "", "")
	v, err := h.Parse("4096M")
	if err != nil {
		t.Fatalf("Parse(4096M): %v", err)
	}
	if v != int64(4096*1024*1024) {
		t.Errorf("Parse(4096M) = %v, want %d", v, int64(4096*1024*1024))
	}
}

func TestUnitNumberOptionHandlerK(t *testing.T) {
	h := NewUnitNumberOptionHandler("timeout", "0", "", "")
	v, err := h.Parse("4096K")
	if err != nil {
		t.Fatalf("Parse(4096K): %v", err)
	}
	if v != int64(4096*1024) {
		t.Errorf("Parse(4096K) = %v, want %d", v, int64(4096*1024))
	}
}

func TestUnitNumberOptionHandlerLowerK(t *testing.T) {
	h := NewUnitNumberOptionHandler("timeout", "0", "", "")
	v, err := h.Parse("1024k")
	if err != nil {
		t.Fatalf("Parse(1024k): %v", err)
	}
	if v != int64(1024*1024) {
		t.Errorf("Parse(1024k) = %v, want %d", v, int64(1024*1024))
	}
}

func TestUnitNumberOptionHandlerLowerM(t *testing.T) {
	h := NewUnitNumberOptionHandler("timeout", "0", "", "")
	v, err := h.Parse("10m")
	if err != nil {
		t.Fatalf("Parse(10m): %v", err)
	}
	if v != int64(10*1024*1024) {
		t.Errorf("Parse(10m) = %v, want %d", v, int64(10*1024*1024))
	}
}

func TestUnitNumberOptionHandlerBareK(t *testing.T) {
	h := NewUnitNumberOptionHandler("timeout", "0", "", "")
	_, err := h.Parse("K")
	if err == nil {
		t.Fatal("Parse(K) returned nil, want error")
	}
}

func TestUnitNumberOptionHandlerBareM(t *testing.T) {
	h := NewUnitNumberOptionHandler("timeout", "0", "", "")
	_, err := h.Parse("M")
	if err == nil {
		t.Fatal("Parse(M) returned nil, want error")
	}
}

func TestUnitNumberOptionHandlerEmpty(t *testing.T) {
	h := NewUnitNumberOptionHandler("timeout", "0", "", "")
	_, err := h.Parse("")
	if err == nil {
		t.Fatal("Parse(\"\") returned nil, want error")
	}
}

func TestUnitNumberOptionHandlerInvalidSuffix(t *testing.T) {
	h := NewUnitNumberOptionHandler("timeout", "0", "", "")
	_, err := h.Parse("4096G")
	if err == nil {
		t.Fatal("Parse(4096G) returned nil, want error (G suffix not supported)")
	}
}

func TestUnitNumberOptionHandlerNegative(t *testing.T) {
	h := NewUnitNumberOptionHandler("timeout", "0", "", "")
	_, err := h.Parse("-1")
	if err == nil {
		t.Fatal("Parse(-1) returned nil, want error")
	}
}

func TestUnitNumberOptionHandlerNegativeK(t *testing.T) {
	h := NewUnitNumberOptionHandler("timeout", "0", "", "")
	_, err := h.Parse("-1K")
	if err == nil {
		t.Fatal("Parse(-1K) returned nil, want error")
	}
}

func TestUnitNumberOptionHandlerZero(t *testing.T) {
	h := NewUnitNumberOptionHandler("timeout", "0", "", "")
	v, err := h.Parse("0")
	if err != nil {
		t.Fatalf("Parse(0): %v", err)
	}
	if v != int64(0) {
		t.Errorf("Parse(0) = %v, want 0", v)
	}
}

func TestUnitNumberOptionHandlerMinBound(t *testing.T) {
	h := NewUnitNumberOptionHandler("timeout", "0", "1024", "")
	v, err := h.Parse("1024")
	if err != nil {
		t.Fatalf("Parse(1024) with min=1024: %v", err)
	}
	if v != int64(1024) {
		t.Errorf("Parse(1024) = %v, want 1024", v)
	}
	_, err = h.Parse("512")
	if err == nil {
		t.Fatal("Parse(512) with min=1024 returned nil, want error")
	}
}

func TestUnitNumberOptionHandlerMaxBound(t *testing.T) {
	h := NewUnitNumberOptionHandler("timeout", "0", "", "1048576")
	_, err := h.Parse("1048576")
	if err != nil {
		t.Fatalf("Parse(1048576) with max=1048576: %v", err)
	}
	_, err = h.Parse("1048577")
	if err == nil {
		t.Fatal("Parse(1048577) with max=1048576 returned nil, want error")
	}
}

func TestUnitNumberOptionHandlerDefault(t *testing.T) {
	h := NewUnitNumberOptionHandler("disk-cache", "16M", "", "")
	if h.DefaultValue() != int64(16*1024*1024) {
		t.Errorf("DefaultValue() = %v, want %d", h.DefaultValue(), int64(16*1024*1024))
	}
}

// --- ParameterOptionHandler ---

func TestParameterOptionHandlerValid(t *testing.T) {
	h := NewParameterOptionHandler("timeout", "", []string{"value1", "value2"})
	v, err := h.Parse("value1")
	if err != nil {
		t.Fatalf("Parse(value1): %v", err)
	}
	if v != "value1" {
		t.Errorf("Parse(value1) = %v, want value1", v)
	}
	v, err = h.Parse("value2")
	if err != nil {
		t.Fatalf("Parse(value2): %v", err)
	}
	if v != "value2" {
		t.Errorf("Parse(value2) = %v, want value2", v)
	}
}

func TestParameterOptionHandlerInvalid(t *testing.T) {
	h := NewParameterOptionHandler("timeout", "", []string{"value1", "value2"})
	_, err := h.Parse("value3")
	if err == nil {
		t.Fatal("Parse(value3) returned nil, want error")
	}
	cfgErr, ok := err.(*Error)
	if !ok {
		t.Fatalf("error is %T, want *config.Error", err)
	}
	if cfgErr.Code != ErrInvalidOption {
		t.Errorf("error code = %d, want %d", cfgErr.Code, ErrInvalidOption)
	}
}

func TestParameterOptionHandlerAllowedValues(t *testing.T) {
	h := NewParameterOptionHandler("timeout", "", []string{"value1", "value2"})
	allowed := h.AllowedValues()
	if len(allowed) != 2 || allowed[0] != "value1" || allowed[1] != "value2" {
		t.Errorf("AllowedValues() = %v, want [value1 value2]", allowed)
	}
}

func TestParameterOptionHandlerDefault(t *testing.T) {
	h := NewParameterOptionHandler("timeout", "notice", []string{"debug", "info", "notice"})
	if h.DefaultValue() != "notice" {
		t.Errorf("DefaultValue() = %v, want notice", h.DefaultValue())
	}
}

// --- FloatNumberOptionHandler ---

func TestFloatNumberOptionHandler(t *testing.T) {
	h := NewFloatNumberOptionHandler("timeout", 0.0, -1.0, -1.0)
	v, err := h.Parse("1.0")
	if err != nil {
		t.Fatalf("Parse(1.0): %v", err)
	}
	if v != 1.0 {
		t.Errorf("Parse(1.0) = %v, want 1.0", v)
	}
}

func TestFloatNumberOptionHandlerAllowedValuesUnbounded(t *testing.T) {
	h := NewFloatNumberOptionHandler("timeout", 0.0, -1.0, -1.0)
	allowed := h.AllowedValues()
	if len(allowed) != 1 || allowed[0] != "*-*" {
		t.Errorf("AllowedValues() = %v, want [*-*]", allowed)
	}
}

func TestFloatNumberOptionHandlerMin(t *testing.T) {
	h := NewFloatNumberOptionHandler("timeout", 0.0, 0.0, -1.0)
	v, err := h.Parse("0.0")
	if err != nil {
		t.Fatalf("Parse(0.0): %v", err)
	}
	if v != 0.0 {
		t.Errorf("Parse(0.0) = %v, want 0.0", v)
	}
	_, err = h.Parse("-0.1")
	if err == nil {
		t.Fatal("Parse(-0.1) with min=0.0 returned nil, want error")
	}
}

func TestFloatNumberOptionHandlerAllowedValuesMin(t *testing.T) {
	h := NewFloatNumberOptionHandler("timeout", 0.0, 0.0, -1.0)
	allowed := h.AllowedValues()
	if len(allowed) != 1 || allowed[0] != "0.0-*" {
		t.Errorf("AllowedValues() = %v, want [0.0-*]", allowed)
	}
}

func TestFloatNumberOptionHandlerMax(t *testing.T) {
	h := NewFloatNumberOptionHandler("timeout", 0.0, -1.0, 10.0)
	v, err := h.Parse("10.0")
	if err != nil {
		t.Fatalf("Parse(10.0): %v", err)
	}
	if v != 10.0 {
		t.Errorf("Parse(10.0) = %v, want 10.0", v)
	}
	_, err = h.Parse("10.1")
	if err == nil {
		t.Fatal("Parse(10.1) with max=10.0 returned nil, want error")
	}
}

func TestFloatNumberOptionHandlerAllowedValuesMax(t *testing.T) {
	h := NewFloatNumberOptionHandler("timeout", 0.0, -1.0, 10.0)
	allowed := h.AllowedValues()
	if len(allowed) != 1 || allowed[0] != "*-10.0" {
		t.Errorf("AllowedValues() = %v, want [*-10.0]", allowed)
	}
}

func TestFloatNumberOptionHandlerMinMax(t *testing.T) {
	h := NewFloatNumberOptionHandler("timeout", 0.0, 0.0, 10.0)
	v, err := h.Parse("0.0")
	if err != nil {
		t.Fatalf("Parse(0.0): %v", err)
	}
	if v != 0.0 {
		t.Errorf("Parse(0.0) = %v, want 0.0", v)
	}
	v, err = h.Parse("10.0")
	if err != nil {
		t.Fatalf("Parse(10.0): %v", err)
	}
	if v != 10.0 {
		t.Errorf("Parse(10.0) = %v, want 10.0", v)
	}
	_, err = h.Parse("-0.1")
	if err == nil {
		t.Fatal("Parse(-0.1) with min=0.0 returned nil, want error")
	}
	_, err = h.Parse("10.1")
	if err == nil {
		t.Fatal("Parse(10.1) with max=10.0 returned nil, want error")
	}
}

func TestFloatNumberOptionHandlerAllowedValuesMinMax(t *testing.T) {
	h := NewFloatNumberOptionHandler("timeout", 0.0, 0.0, 10.0)
	allowed := h.AllowedValues()
	if len(allowed) != 1 || allowed[0] != "0.0-10.0" {
		t.Errorf("AllowedValues() = %v, want [0.0-10.0]", allowed)
	}
}

func TestFloatNumberOptionHandlerBadInput(t *testing.T) {
	h := NewFloatNumberOptionHandler("timeout", 0.0, -1.0, -1.0)
	_, err := h.Parse("abc")
	if err == nil {
		t.Fatal("Parse(abc) returned nil, want error")
	}
}

func TestFloatNumberOptionHandlerDefault(t *testing.T) {
	h := NewFloatNumberOptionHandler("seed-ratio", 1.0, 0.0, -1.0)
	if h.DefaultValue() != 1.0 {
		t.Errorf("DefaultValue() = %v, want 1.0", h.DefaultValue())
	}
}

// --- HttpProxyOptionHandler ---

func TestHttpProxyOptionHandlerEmpty(t *testing.T) {
	h := NewHttpProxyOptionHandler("http-proxy", "")
	v, err := h.Parse("")
	if err != nil {
		t.Fatalf("Parse(\"\"): %v", err)
	}
	if v != "" {
		t.Errorf("Parse(\"\") = %v, want empty string", v)
	}
}

func TestHttpProxyOptionHandlerHostPort(t *testing.T) {
	h := NewHttpProxyOptionHandler("http-proxy", "")
	v, err := h.Parse("proxy:65535")
	if err != nil {
		t.Fatalf("Parse(proxy:65535): %v", err)
	}
	if v != "http://proxy:65535/" {
		t.Errorf("Parse(proxy:65535) = %q, want http://proxy:65535/", v)
	}
}

func TestHttpProxyOptionHandlerFullURL(t *testing.T) {
	h := NewHttpProxyOptionHandler("http-proxy", "")
	v, err := h.Parse("http://proxy:8080")
	if err != nil {
		t.Fatalf("Parse(http://proxy:8080): %v", err)
	}
	if v != "http://proxy:8080/" {
		t.Errorf("Parse(http://proxy:8080) = %q, want http://proxy:8080/", v)
	}
}

func TestHttpProxyOptionHandlerUserPass(t *testing.T) {
	h := NewHttpProxyOptionHandler("http-proxy", "")
	v, err := h.Parse("http://user%40:passwd%40@proxy:8080")
	if err != nil {
		t.Fatalf("Parse(http://user%%40:passwd%%40@proxy:8080): %v", err)
	}
	if v != "http://user%40:passwd%40@proxy:8080/" {
		t.Errorf("Parse(...) = %q, want http://user%%40:passwd%%40@proxy:8080/", v)
	}
}

func TestHttpProxyOptionHandlerIPv6(t *testing.T) {
	h := NewHttpProxyOptionHandler("http-proxy", "")
	v, err := h.Parse("http://[::1]:8080")
	if err != nil {
		t.Fatalf("Parse(http://[::1]:8080): %v", err)
	}
	if v != "http://[::1]:8080/" {
		t.Errorf("Parse(http://[::1]:8080) = %q, want http://[::1]:8080/", v)
	}
}

func TestHttpProxyOptionHandlerPortTooHigh(t *testing.T) {
	h := NewHttpProxyOptionHandler("http-proxy", "")
	_, err := h.Parse("http://bar:65536")
	if err == nil {
		t.Fatal("Parse(http://bar:65536) returned nil, want error")
	}
}

func TestHttpProxyOptionHandlerPortZero(t *testing.T) {
	h := NewHttpProxyOptionHandler("http-proxy", "")
	_, err := h.Parse("http://bar:0")
	if err == nil {
		t.Fatal("Parse(http://bar:0) returned nil, want error")
	}
}

func TestHttpProxyOptionHandlerAllowedValues(t *testing.T) {
	h := NewHttpProxyOptionHandler("http-proxy", "")
	allowed := h.AllowedValues()
	if len(allowed) != 1 || allowed[0] != "[http://][USER:PASSWORD@]HOST[:PORT]" {
		t.Errorf("AllowedValues() = %v, want [[http://][USER:PASSWORD@]HOST[:PORT]]", allowed)
	}
}

// --- DeprecatedOptionHandler ---

func TestDeprecatedOptionHandlerWithReplacement(t *testing.T) {
	replacement := NewNumberOptionHandler("dir", 0, -1, -1) // dir is string in real, but we use Number for test
	d := NewDeprecatedOptionHandler("old-option", replacement)
	v, err := d.Parse("42")
	if err != nil {
		t.Fatalf("Parse(42): %v", err)
	}
	if v != int64(42) {
		t.Errorf("Parse(42) = %v, want 42", v)
	}
}

func TestDeprecatedOptionHandlerNoReplacement(t *testing.T) {
	d := NewDeprecatedOptionHandler("old-option", nil)
	v, err := d.Parse("foo")
	if err != nil {
		t.Fatalf("Parse(foo): %v", err)
	}
	if v != nil {
		t.Errorf("Parse(foo) = %v, want nil", v)
	}
}

func TestDeprecatedOptionHandlerDelegatesToReplacement(t *testing.T) {
	replacement := NewParameterOptionHandler("new-option", "a", []string{"a", "b"})
	d := NewDeprecatedOptionHandler("old-option", replacement)

	v, err := d.Parse("a")
	if err != nil {
		t.Fatalf("Parse(a): %v", err)
	}
	if v != "a" {
		t.Errorf("Parse(a) = %v, want a", v)
	}

	_, err = d.Parse("c")
	if err == nil {
		t.Fatal("Parse(c) should fail through replacement handler")
	}
}

func TestDeprecatedOptionHandlerName(t *testing.T) {
	d := NewDeprecatedOptionHandler("old-option", nil)
	if d.Name() != "old-option" {
		t.Errorf("Name() = %q, want old-option", d.Name())
	}
}

func TestDeprecatedOptionHandlerDefaultValue(t *testing.T) {
	replacement := NewBooleanOptionHandler("new-option", true)
	d := NewDeprecatedOptionHandler("old-option", replacement)
	if d.DefaultValue() != true {
		t.Errorf("DefaultValue() = %v, want true", d.DefaultValue())
	}
}

func TestDeprecatedOptionHandlerDefaultValueNoReplacement(t *testing.T) {
	d := NewDeprecatedOptionHandler("old-option", nil)
	if d.DefaultValue() != nil {
		t.Errorf("DefaultValue() = %v, want nil", d.DefaultValue())
	}
}

func TestDeprecatedOptionHandlerAllowedValues(t *testing.T) {
	replacement := NewParameterOptionHandler("new", "a", []string{"a", "b"})
	d := NewDeprecatedOptionHandler("old", replacement)
	allowed := d.AllowedValues()
	if len(allowed) != 2 || allowed[0] != "a" || allowed[1] != "b" {
		t.Errorf("AllowedValues() = %v, want [a b]", allowed)
	}
}

func TestDeprecatedOptionHandlerAllowedValuesNoReplacement(t *testing.T) {
	d := NewDeprecatedOptionHandler("old", nil)
	allowed := d.AllowedValues()
	if allowed != nil {
		t.Errorf("AllowedValues() = %v, want nil", allowed)
	}
}

// --- unit parsing edge cases ---

func TestParseUnitEZero(t *testing.T) {
	v, err := parseUnitE("0")
	if err != nil {
		t.Fatalf("parseUnitE(0): %v", err)
	}
	if v != 0 {
		t.Errorf("parseUnitE(0) = %d, want 0", v)
	}
}

func TestParseUnitENoAnnotation(t *testing.T) {
	v, err := parseUnitE("1024")
	if err != nil {
		t.Fatalf("parseUnitE(1024): %v", err)
	}
	if v != 1024 {
		t.Errorf("parseUnitE(1024) = %d, want 1024", v)
	}
}

func TestParseUnitE1K(t *testing.T) {
	v, err := parseUnitE("1K")
	if err != nil {
		t.Fatalf("parseUnitE(1K): %v", err)
	}
	if v != 1024 {
		t.Errorf("parseUnitE(1K) = %d, want 1024", v)
	}
}

func TestParseUnitE1k(t *testing.T) {
	v, err := parseUnitE("1k")
	if err != nil {
		t.Fatalf("parseUnitE(1k): %v", err)
	}
	if v != 1024 {
		t.Errorf("parseUnitE(1k) = %d, want 1024", v)
	}
}

func TestParseUnitE1M(t *testing.T) {
	v, err := parseUnitE("1M")
	if err != nil {
		t.Fatalf("parseUnitE(1M): %v", err)
	}
	if v != 1048576 {
		t.Errorf("parseUnitE(1M) = %d, want 1048576", v)
	}
}

func TestParseUnitE1m(t *testing.T) {
	v, err := parseUnitE("1m")
	if err != nil {
		t.Fatalf("parseUnitE(1m): %v", err)
	}
	if v != 1048576 {
		t.Errorf("parseUnitE(1m) = %d, want 1048576", v)
	}
}

func TestParseUnitENegative(t *testing.T) {
	_, err := parseUnitE("-1024")
	if err == nil {
		t.Fatal("parseUnitE(-1024) returned nil, want error")
	}
}

// --- parseUnit constructor panics ---

func TestParseUnitPanicsOnInvalid(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("parseUnit(\"\") should panic")
		}
	}()
	parseUnit("")
}

// --- Error messages ---

func TestNumberOptionHandlerErrorMessageMin(t *testing.T) {
	h := NewNumberOptionHandler("split", 5, 1, -1)
	_, err := h.Parse("0")
	if err == nil {
		t.Fatal("expected error")
	}
	cfgErr := err.(*Error)
	if cfgErr.Msg != "split must be greater than or equal to 1." {
		t.Errorf("error msg = %q", cfgErr.Msg)
	}
}

func TestNumberOptionHandlerErrorMessageMax(t *testing.T) {
	h := NewNumberOptionHandler("timeout", 60, -1, 600)
	_, err := h.Parse("601")
	if err == nil {
		t.Fatal("expected error")
	}
	cfgErr := err.(*Error)
	if cfgErr.Msg != "timeout must be smaller than or equal to 600." {
		t.Errorf("error msg = %q", cfgErr.Msg)
	}
}

func TestNumberOptionHandlerErrorMessageBetween(t *testing.T) {
	h := NewNumberOptionHandler("timeout", 60, 1, 600)
	_, err := h.Parse("0")
	if err == nil {
		t.Fatal("expected error")
	}
	cfgErr := err.(*Error)
	if cfgErr.Msg != "timeout must be between 1 and 600." {
		t.Errorf("error msg = %q", cfgErr.Msg)
	}
}

func TestFloatOptionHandlerErrorMessageBetween(t *testing.T) {
	h := NewFloatNumberOptionHandler("timeout", 0.0, 0.0, 10.0)
	_, err := h.Parse("-0.1")
	if err == nil {
		t.Fatal("expected error")
	}
	cfgErr := err.(*Error)
	if cfgErr.Msg != "timeout must be between 0.0 and 10.0." {
		t.Errorf("error msg = %q", cfgErr.Msg)
	}
}

func TestFloatOptionHandlerErrorMessageMin(t *testing.T) {
	h := NewFloatNumberOptionHandler("timeout", 0.0, 0.0, -1.0)
	_, err := h.Parse("-0.1")
	if err == nil {
		t.Fatal("expected error")
	}
	cfgErr := err.(*Error)
	if cfgErr.Msg != "timeout must be greater than or equal to 0.0." {
		t.Errorf("error msg = %q", cfgErr.Msg)
	}
}

func TestFloatOptionHandlerErrorMessageMax(t *testing.T) {
	h := NewFloatNumberOptionHandler("timeout", 0.0, -1.0, 10.0)
	_, err := h.Parse("10.1")
	if err == nil {
		t.Fatal("expected error")
	}
	cfgErr := err.(*Error)
	if cfgErr.Msg != "timeout must be smaller than or equal to 10.0." {
		t.Errorf("error msg = %q", cfgErr.Msg)
	}
}

func TestBooleanOptionHandlerErrorMessage(t *testing.T) {
	h := NewBooleanOptionHandler("daemon", false)
	_, err := h.Parse("yes")
	if err == nil {
		t.Fatal("expected error")
	}
	cfgErr := err.(*Error)
	if cfgErr.Msg != "daemon must be either 'true' or 'false'." {
		t.Errorf("error msg = %q", cfgErr.Msg)
	}
}

func TestParameterOptionHandlerErrorMessage(t *testing.T) {
	h := NewParameterOptionHandler("ftp-type", "binary", []string{"binary", "ascii"})
	_, err := h.Parse("ebcdic")
	if err == nil {
		t.Fatal("expected error")
	}
	cfgErr := err.(*Error)
	if cfgErr.Msg != "ftp-type must be one of the following:'binary' 'ascii'" {
		t.Errorf("error msg = %q", cfgErr.Msg)
	}
}
