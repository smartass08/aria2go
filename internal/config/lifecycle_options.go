package config

import "runtime"

func validEventPollValues() []string {
	switch runtime.GOOS {
	case "linux", "android":
		return []string{"epoll", "poll", "select"}
	case "darwin", "freebsd", "openbsd", "netbsd", "dragonfly":
		return []string{"kqueue", "poll", "select"}
	case "solaris", "illumos":
		return []string{"port", "poll", "select"}
	default:
		return []string{"poll", "select"}
	}
}

func isValidEventPollValue(value string) bool {
	for _, allowed := range validEventPollValues() {
		if value == allowed {
			return true
		}
	}
	return false
}
