package metrics

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
)

const ticksPerSecond = 100

type procStats struct {
	UserTicks uint64
	SysTicks  uint64
	RSSBytes  int64
	VSSBytes  int64
	Threads   int
	OpenFDs   int
}

func readProcessStats(pid int) (procStats, error) {
	if runtime.GOOS == "linux" {
		return readLinux(pid)
	}
	return readDarwin(pid)
}

func readLinux(pid int) (procStats, error) {
	var s procStats

	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return s, err
	}
	fields := splitProcStat(string(stat))
	if len(fields) < 52 {
		return s, fmt.Errorf("short /proc/%d/stat", pid)
	}
	s.UserTicks, _ = strconv.ParseUint(fields[13], 10, 64)
	s.SysTicks, _ = strconv.ParseUint(fields[14], 10, 64)
	s.Threads, _ = strconv.Atoi(fields[19])
	s.VSSBytes, _ = strconv.ParseInt(fields[22], 10, 64)
	rssPages, _ := strconv.ParseInt(fields[23], 10, 64)
	s.RSSBytes = rssPages * 4096

	if entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid)); err == nil {
		s.OpenFDs = len(entries)
	}
	return s, nil
}

func splitProcStat(line string) []string {
	open := strings.IndexByte(line, '(')
	close := strings.LastIndexByte(line, ')')
	if open < 0 || close < 0 || close <= open {
		return strings.Fields(line)
	}
	head := strings.TrimSpace(line[:open])
	tail := strings.TrimSpace(line[close+1:])
	out := strings.Fields(head)
	out = append(out, "COMM")
	out = append(out, strings.Fields(tail)...)
	return out
}

func readDarwin(pid int) (procStats, error) {
	var s procStats
	out, err := execPS(pid)
	if err != nil {
		return s, err
	}
	fields := strings.Fields(out)
	if len(fields) < 3 {
		return s, fmt.Errorf("ps output too short: %q", out)
	}
	rssKb, _ := strconv.ParseInt(fields[0], 10, 64)
	s.RSSBytes = rssKb * 1024
	vsKb, _ := strconv.ParseInt(fields[1], 10, 64)
	s.VSSBytes = vsKb * 1024
	cpuPct, _ := strconv.ParseFloat(fields[2], 64)
	s.UserTicks = uint64(cpuPct * float64(ticksPerSecond) / 100)
	return s, nil
}
