package config

import (
	"fmt"
	"net"
	"strconv"
)

func parseDHTEntryPointValue(name, value string) (host, port string, err error) {
	host, port, err = net.SplitHostPort(value)
	if err != nil {
		return "", "", &Error{
			Code: ErrInvalidOption,
			Msg:  fmt.Sprintf("%s must be in HOST:PORT format, got %q", name, value),
		}
	}
	if host == "" {
		return "", "", &Error{
			Code: ErrInvalidOption,
			Msg:  fmt.Sprintf("%s host must not be empty", name),
		}
	}
	if err := validateDHTEntryPointPort(name, port); err != nil {
		return "", "", err
	}
	return host, port, nil
}

func validateDHTEntryPointPort(name, port string) error {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return &Error{
			Code: ErrInvalidOption,
			Msg:  fmt.Sprintf("%s port must be between 1 and 65535, got %q", name, port),
		}
	}
	return nil
}
