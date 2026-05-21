package config

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
)

// ConfigValueParser validates and parses a single option value string.
type ConfigValueParser interface {
	Parse(s string) (interface{}, error)
	Name() string
	DefaultValue() interface{}
	AllowedValues() []string
}

// BooleanHandler validates "true"/"false" strings.
type BooleanHandler struct {
	name         string
	defaultValue bool
}

// NewBooleanOptionHandler creates a BooleanHandler.
func NewBooleanOptionHandler(name string, defaultVal bool) *BooleanHandler {
	return &BooleanHandler{name: name, defaultValue: defaultVal}
}

func (h *BooleanHandler) Parse(s string) (interface{}, error) {
	switch s {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return nil, &Error{
			Code: ErrInvalidOption,
			Msg:  fmt.Sprintf("%s must be either 'true' or 'false'.", h.name),
		}
	}
}

func (h *BooleanHandler) Name() string              { return h.name }
func (h *BooleanHandler) DefaultValue() interface{} { return h.defaultValue }
func (h *BooleanHandler) AllowedValues() []string   { return []string{"true", "false"} }

// NumberHandler validates an integer within [min, max].
// Use -1 for min or max to indicate unbounded.
type NumberHandler struct {
	name         string
	defaultValue int64
	min          int64
	max          int64
}

// NewNumberOptionHandler creates a NumberHandler.
// min=-1 means no lower bound; max=-1 means no upper bound.
func NewNumberOptionHandler(name string, defaultVal int64, min, max int64) *NumberHandler {
	return &NumberHandler{name: name, defaultValue: defaultVal, min: min, max: max}
}

func (h *NumberHandler) Parse(s string) (interface{}, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, &Error{
			Code: ErrInvalidOption,
			Msg:  fmt.Sprintf("Bad number %s", s),
		}
	}
	if (h.min == -1 || h.min <= n) && (h.max == -1 || n <= h.max) {
		return n, nil
	}
	return nil, &Error{
		Code: ErrInvalidOption,
		Msg:  h.buildRangeError(n),
	}
}

func (h *NumberHandler) buildRangeError(n int64) string {
	msg := h.name + " "
	switch {
	case h.min == -1 && h.max != -1:
		msg += fmt.Sprintf("must be smaller than or equal to %d.", h.max)
	case h.min != -1 && h.max != -1:
		msg += fmt.Sprintf("must be between %d and %d.", h.min, h.max)
	case h.min != -1 && h.max == -1:
		msg += fmt.Sprintf("must be greater than or equal to %d.", h.min)
	default:
		msg += "must be a number."
	}
	return msg
}

func (h *NumberHandler) Name() string              { return h.name }
func (h *NumberHandler) DefaultValue() interface{} { return h.defaultValue }

func (h *NumberHandler) AllowedValues() []string {
	minStr := "*"
	if h.min != -1 {
		minStr = strconv.FormatInt(h.min, 10)
	}
	maxStr := "*"
	if h.max != -1 {
		maxStr = strconv.FormatInt(h.max, 10)
	}
	return []string{minStr + "-" + maxStr}
}

// UnitHandler parses size strings with optional K/M suffix (1024-based, no G).
type UnitHandler struct {
	name         string
	defaultValue int64
	min          int64
	max          int64
}

// NewUnitNumberOptionHandler creates a UnitHandler.
// defaultVal is a unit string like "16M". min/max are unit strings or "" for unbounded.
func NewUnitNumberOptionHandler(name string, defaultVal string, min, max string) *UnitHandler {
	def := parseUnit(defaultVal)
	var minVal, maxVal int64 = -1, -1
	if min != "" {
		minVal = parseUnit(min)
	}
	if max != "" {
		maxVal = parseUnit(max)
	}
	return &UnitHandler{name: name, defaultValue: def, min: minVal, max: maxVal}
}

func (h *UnitHandler) Parse(s string) (interface{}, error) {
	n, err := parseUnitE(s)
	if err != nil {
		return nil, err
	}
	if (h.min == -1 || h.min <= n) && (h.max == -1 || n <= h.max) {
		return n, nil
	}
	msg := h.name + " "
	switch {
	case h.min == -1 && h.max != -1:
		msg += fmt.Sprintf("must be smaller than or equal to %d.", h.max)
	case h.min != -1 && h.max != -1:
		msg += fmt.Sprintf("must be between %d and %d.", h.min, h.max)
	case h.min != -1 && h.max == -1:
		msg += fmt.Sprintf("must be greater than or equal to %d.", h.min)
	default:
		msg += "must be a number."
	}
	return nil, &Error{Code: ErrInvalidOption, Msg: msg}
}

func (h *UnitHandler) Name() string              { return h.name }
func (h *UnitHandler) DefaultValue() interface{} { return h.defaultValue }

func (h *UnitHandler) AllowedValues() []string {
	// Unit handlers show the min-max range (resolved to int64 bounds).
	minStr := "*"
	if h.min != -1 {
		minStr = strconv.FormatInt(h.min, 10)
	}
	maxStr := "*"
	if h.max != -1 {
		maxStr = strconv.FormatInt(h.max, 10)
	}
	return []string{minStr + "-" + maxStr}
}

// parseUnit parses a size string like "4096M", "1K", "1024" and returns the integer value.
// It panics on invalid input (programmer error during construction).
func parseUnit(s string) int64 {
	v, err := parseUnitE(s)
	if err != nil {
		panic(fmt.Sprintf("config: invalid unit value: %s", s))
	}
	return v
}

// parseUnitE parses a size string with K/k (×1024) or M/m (×1024²) suffix.
// Returns an error for empty strings, negative values, invalid suffixes, or bare "K"/"M".
func parseUnitE(s string) (int64, error) {
	if s == "" {
		return 0, &Error{Code: ErrInvalidOption, Msg: fmt.Sprintf("Bad or negative value detected: %s", s)}
	}
	mult := int64(1)
	last := s[len(s)-1]
	switch last {
	case 'K', 'k':
		mult = 1024
		s = s[:len(s)-1]
	case 'M', 'm':
		mult = 1024 * 1024
		s = s[:len(s)-1]
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0, &Error{Code: ErrInvalidOption, Msg: fmt.Sprintf("Bad or negative value detected: %s", s)}
	}
	if n > math.MaxInt64/mult {
		return 0, &Error{Code: ErrInvalidOption, Msg: "overflow/underflow"}
	}
	return n * mult, nil
}

// FloatHandler validates a float within [min, max].
// Use a negative value for min or max to indicate unbounded.
type FloatHandler struct {
	name         string
	defaultValue float64
	min          float64
	max          float64
}

// NewFloatNumberOptionHandler creates a FloatHandler.
// min < 0 means no lower bound; max < 0 means no upper bound.
func NewFloatNumberOptionHandler(name string, defaultVal float64, min, max float64) *FloatHandler {
	return &FloatHandler{name: name, defaultValue: defaultVal, min: min, max: max}
}

func (h *FloatHandler) Parse(s string) (interface{}, error) {
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, &Error{
			Code: ErrInvalidOption,
			Msg:  fmt.Sprintf("Bad number %s", s),
		}
	}
	if (h.min < 0 || h.min <= n) && (h.max < 0 || n <= h.max) {
		return n, nil
	}
	msg := h.name + " "
	switch {
	case h.min < 0 && h.max >= 0:
		msg += fmt.Sprintf("must be smaller than or equal to %.1f.", h.max)
	case h.min >= 0 && h.max >= 0:
		msg += fmt.Sprintf("must be between %.1f and %.1f.", h.min, h.max)
	case h.min >= 0 && h.max < 0:
		msg += fmt.Sprintf("must be greater than or equal to %.1f.", h.min)
	default:
		msg += "must be a number."
	}
	return nil, &Error{Code: ErrInvalidOption, Msg: msg}
}

func (h *FloatHandler) Name() string              { return h.name }
func (h *FloatHandler) DefaultValue() interface{} { return h.defaultValue }

func (h *FloatHandler) AllowedValues() []string {
	minStr := "*"
	if h.min >= 0 {
		minStr = fmt.Sprintf("%.1f", h.min)
	}
	maxStr := "*"
	if h.max >= 0 {
		maxStr = fmt.Sprintf("%.1f", h.max)
	}
	return []string{minStr + "-" + maxStr}
}

// ParameterHandler validates that the value is one of the allowed values.
type ParameterHandler struct {
	name         string
	defaultValue string
	allowed      []string
}

// NewParameterOptionHandler creates a ParameterHandler.
func NewParameterOptionHandler(name string, defaultVal string, allowed []string) *ParameterHandler {
	return &ParameterHandler{name: name, defaultValue: defaultVal, allowed: allowed}
}

func (h *ParameterHandler) Parse(s string) (interface{}, error) {
	for _, a := range h.allowed {
		if s == a {
			return s, nil
		}
	}
	msg := h.name + " must be one of the following:"
	if len(h.allowed) == 0 {
		msg += "''"
	} else {
		for _, p := range h.allowed {
			msg += "'" + p + "' "
		}
	}
	msg = strings.TrimRight(msg, " ")
	return nil, &Error{Code: ErrInvalidOption, Msg: msg}
}

func (h *ParameterHandler) Name() string              { return h.name }
func (h *ParameterHandler) DefaultValue() interface{} { return h.defaultValue }
func (h *ParameterHandler) AllowedValues() []string {
	result := make([]string, len(h.allowed))
	copy(result, h.allowed)
	return result
}

// ProxyHandler validates proxy URLs.
type ProxyHandler struct {
	name         string
	defaultValue string
}

// NewHttpProxyOptionHandler creates a ProxyHandler.
func NewHttpProxyOptionHandler(name string, defaultVal string) *ProxyHandler {
	return &ProxyHandler{name: name, defaultValue: defaultVal}
}

func (h *ProxyHandler) Parse(s string) (interface{}, error) {
	if s == "" {
		return "", nil
	}
	if !strings.Contains(s, "://") {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return nil, &Error{Code: ErrInvalidOption, Msg: "unrecognized proxy format"}
	}
	if u.Port() != "" {
		port, err := strconv.Atoi(u.Port())
		if err != nil || port < 1 || port > 65535 {
			return nil, &Error{Code: ErrInvalidOption, Msg: "unrecognized proxy format"}
		}
	}
	u.Scheme = "http"
	result := u.String()
	if !strings.HasSuffix(result, "/") {
		result += "/"
	}
	return result, nil
}

func (h *ProxyHandler) Name() string              { return h.name }
func (h *ProxyHandler) DefaultValue() interface{} { return h.defaultValue }

func (h *ProxyHandler) AllowedValues() []string {
	return []string{"[http://][USER:PASSWORD@]HOST[:PORT]"}
}

// DeprecatedHandler redirects to a replacement handler, logging a warning.
type DeprecatedHandler struct {
	name           string
	replacement    ConfigValueParser
	hasReplacement bool
	stillWork      bool
}

// NewDeprecatedOptionHandler creates a DeprecatedHandler.
// If replacement is an empty string, the option has no replacement and is fully deprecated.
func NewDeprecatedOptionHandler(name string, replacement ConfigValueParser) *DeprecatedHandler {
	return &DeprecatedHandler{
		name:           name,
		replacement:    replacement,
		hasReplacement: replacement != nil,
	}
}

func (h *DeprecatedHandler) Parse(s string) (interface{}, error) {
	if h.hasReplacement && h.replacement != nil {
		return h.replacement.Parse(s)
	}
	return nil, nil
}

func (h *DeprecatedHandler) Name() string { return h.name }
func (h *DeprecatedHandler) DefaultValue() interface{} {
	if h.hasReplacement && h.replacement != nil {
		return h.replacement.DefaultValue()
	}
	return nil
}

func (h *DeprecatedHandler) AllowedValues() []string {
	if h.hasReplacement && h.replacement != nil {
		return h.replacement.AllowedValues()
	}
	return nil
}
