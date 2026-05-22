// Package console provides aria2-style terminal output: progress bars,
// signal handling, and an interactive command loop. All output goes to
// stdout by default; Options.Stderr selects aria2's stderr redirection mode.
//
// Progress format comes from the aria2 ConsoleStatCalc reference:
//   - Single download:  [#SIZE_PREFIX SIZE/MAXSIZE(XX%) CN:N SD:N DL:SPEED]
//   - Multiple:         [DL:SPEED UL:SPEED][#PREFIX SIZE/MAXSIZE]...
//   - Summary:          *** Download Progress Summary as of <date> ***
//
// On TTY: output uses \r<content> for overwrite (no trailing newline).
// On non-TTY: output ends with \n (one line per update).
//
// Update throttling: render is limited to 1Hz (1000ms interval), matching
// aria2's ConsoleStatCalc::calculateStat throttle.
package console

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Console is an aria2-style terminal output handle.
type Console struct {
	out         io.Writer
	opts        Options
	sigCh       chan os.Signal
	mu          sync.Mutex
	lastUpdate  time.Time
	lastSummary time.Time
	fileAlloc   *AllocProgress
	checksum    *CheckProgress
}

// Options configures a Console.
type Options struct {
	NoColor         bool
	Quiet           bool
	SummaryInterval time.Duration
	Interactive     bool
	Stderr          bool
	DownloadsDone   bool // true when all downloads finished — compact mode omits DL/UL header
	ShowReadout     bool
	ShowReadoutSet  bool
	Truncate        bool
	TruncateSet     bool
}

// AllocProgress holds file allocation progress for inline display.
// nil means no allocation in progress (stub hidden).
type AllocProgress struct {
	GID         string
	CurrentSize int64
	TotalSize   int64
	Queued      int
}

// CheckProgress holds checksum verification progress for inline display.
// nil means no checksum in progress (stub hidden).
type CheckProgress struct {
	GID         string
	CurrentSize int64
	TotalSize   int64
	Queued      int
}

// New creates a Console attached to stdout unless Options.Stderr is set.
func New(opts Options) *Console {
	out := io.Writer(os.Stdout)
	if opts.Stderr {
		out = os.Stderr
	}
	return NewWithWriter(out, opts)
}

// NewWithWriter creates a Console attached to w.
func NewWithWriter(w io.Writer, opts Options) *Console {
	if w == nil {
		w = io.Discard
	}
	return &Console{out: w, opts: opts}
}

// SetFileAllocProgress sets file allocation progress to display inline.
func (c *Console) SetFileAllocProgress(p *AllocProgress) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fileAlloc = p
}

// SetCheckProgress sets checksum verification progress to display inline.
func (c *Console) SetCheckProgress(p *CheckProgress) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checksum = p
}

// DownloadStat holds a download's current status for progress rendering.
type DownloadStat struct {
	GID                 string
	Status              string
	Progress            float64
	Speed               int64
	UploadSpeed         int64
	TotalSize           int64
	CompletedSize       int64
	AllTimeUploadLength int64 // total bytes uploaded across all sessions (share ratio numerator)
	SessionUploadLength int64 // bytes uploaded in current session (controls UL display visibility)
	Connections         int
	ErrorCode           int
	NumSeeders          int
	Filename            string
	InfoHash            string
	ETA                 int64
	Seeder              bool // true if seeding (aria2: "seeder":"true", status remains "active")
}

// ResultStat holds a stopped download's final state for Download Results.
type ResultStat struct {
	GID          string
	Status       string
	AverageSpeed int64
	Path         string
	Percent      int
}

// color codes matching aria2's ColorizedStream palette.
const (
	clrMagenta = "\033[35m"
	clrGreen   = "\033[32m"
	clrCyan    = "\033[36m"
	clrYellow  = "\033[33m"
	clrClear   = "\033[0m"
)

// Println writes a line followed by a newline to the console.
// In Quiet mode, non-error output is suppressed.
func (c *Console) Println(s string) {
	if c.opts.Quiet {
		return
	}
	fmt.Fprintln(c.out, s)
}

// PrintDownloadResults writes aria2-style final Download Results output.
func (c *Console) PrintDownloadResults(results []ResultStat, full bool) {
	if c.opts.Quiet {
		return
	}

	if full {
		fmt.Fprint(c.out, "\nDownload Results:\n")
		fmt.Fprint(c.out, "gid   |stat|avg speed  |  %|path/URI\n")
		fmt.Fprint(c.out, "======+====+===========+===+===================================================\n")
	} else {
		fmt.Fprint(c.out, "\nDownload Results:\n")
		fmt.Fprint(c.out, "gid   |stat|avg speed  |path/URI\n")
		fmt.Fprint(c.out, "======+====+===========+=======================================================\n")
	}

	var ok, errCount, inProgress, removed int
	for i := range results {
		status := normalizeResultStatus(results[i].Status)
		switch status {
		case "OK":
			ok++
		case "INPR":
			inProgress++
		case "RM":
			removed++
		default:
			errCount++
		}
		c.printDownloadResultRow(results[i], status, full)
	}

	if ok > 0 || errCount > 0 || inProgress > 0 || removed > 0 {
		fmt.Fprint(c.out, "\nStatus Legend:\n")
		if ok > 0 {
			fmt.Fprint(c.out, "(OK):download completed.")
		}
		if errCount > 0 {
			fmt.Fprint(c.out, "(ERR):error occurred.")
		}
		if inProgress > 0 {
			fmt.Fprint(c.out, "(INPR):download in-progress.")
		}
		if removed > 0 {
			fmt.Fprint(c.out, "(RM):download removed.")
		}
		fmt.Fprint(c.out, "\n")
	}
}

func (c *Console) printDownloadResultRow(result ResultStat, status string, full bool) {
	path := result.Path
	if path == "" {
		path = "n/a"
	}
	fmt.Fprintf(c.out, "%-6s|%-4s|%s|", abbrevGID(result.GID), resultStatusText(status), formatResultSpeed(result.AverageSpeed))
	if full {
		if result.Percent >= 0 {
			fmt.Fprintf(c.out, "%3d|", result.Percent)
		} else {
			fmt.Fprint(c.out, "  -|")
		}
	}
	fmt.Fprint(c.out, path)
	fmt.Fprint(c.out, "\n")
}

func normalizeResultStatus(status string) string {
	switch strings.ToUpper(status) {
	case "OK", "COMPLETE", "COMPLETED":
		return "OK"
	case "INPR", "IN_PROGRESS", "IN-PROGRESS", "ACTIVE":
		return "INPR"
	case "RM", "REMOVED":
		return "RM"
	default:
		return "ERR"
	}
}

func resultStatusText(status string) string {
	switch status {
	case "OK":
		return "OK"
	case "INPR":
		return "INPR"
	case "RM":
		return "RM"
	default:
		return "ERR"
	}
}

func formatResultSpeed(speed int64) string {
	if speed < 0 {
		return fmt.Sprintf("%11s", "n/a")
	}
	return fmt.Sprintf("%8sB/s", abbrevSize(speed))
}

// bufPool reuses bytes.Buffer across Render calls to avoid per-call allocations
// on the hot path (1Hz rendering).
var bufPool = sync.Pool{
	New: func() any {
		return new(bytes.Buffer)
	},
}

// Render displays aria2-style download progress for one or more snapshots.
// In Quiet mode this is a no-op. Updates are throttled to 1Hz (1000ms).
func (c *Console) Render(snapshots []DownloadStat) {
	if c.opts.Quiet || len(snapshots) == 0 {
		return
	}

	// 1Hz update throttling, matching aria2 ConsoleStatCalc::calculateStat.
	c.mu.Lock()
	now := time.Now()
	if elapsed := now.Sub(c.lastUpdate); elapsed < 1000*time.Millisecond {
		c.mu.Unlock()
		return
	}
	c.lastUpdate = now
	alloc := c.fileAlloc
	check := c.checksum
	summaryDue := c.opts.SummaryInterval > 0 && (c.lastSummary.IsZero() || now.Sub(c.lastSummary) >= c.opts.SummaryInterval)
	if summaryDue {
		c.lastSummary = now
	}
	c.mu.Unlock()

	tty := c.isatty()
	cols := 79
	if tty {
		if w := terminalWidth(); w > 0 {
			cols = w
		}
		// Clear the terminal line before rendering:
		// \r returns carriage, spaces overwrite old content, \r returns again.
		fmt.Fprintf(c.out, "\r%s\r", strings.Repeat(" ", cols))
	}

	if summaryDue {
		c.renderSummary(snapshots, cols)
	}
	if !c.showReadout() {
		return
	}

	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	if len(snapshots) == 1 {
		c.renderSingle(buf, &snapshots[0])
	} else {
		c.renderCompact(buf, snapshots)
	}

	// Append FileAlloc and Checksum progress stubs (after main progress).
	c.appendAllocProgress(buf, alloc)
	c.appendCheckProgress(buf, check)

	if tty {
		var out string
		if c.opts.NoColor {
			out = stripColors(buf.String())
		} else {
			out = buf.String()
		}
		if c.truncateReadout() {
			out = truncateANSI(out, cols)
		}
		io.WriteString(c.out, out)
	} else {
		// Non-TTY: never use colors, one line per update with trailing newline.
		// Matching aria2's ColorizedStream::str(false) on non-TTY.
		c.writeStripped(c.out, buf)
		fmt.Fprintln(c.out)
	}
	bufPool.Put(buf)
}

// writeStripped writes buf contents to w with ANSI sequences removed,
// avoiding intermediate string allocation.
func (c *Console) writeStripped(w io.Writer, buf *bytes.Buffer) {
	data := buf.Bytes()
	for i := 0; i < len(data); {
		if data[i] == '\033' && i+1 < len(data) && data[i+1] == '[' {
			j := i + 2
			for j < len(data) && data[j] != 'm' {
				j++
			}
			if j < len(data) {
				i = j + 1
				continue
			}
		}
		w.Write(data[i : i+1])
		i++
	}
}

func (c *Console) renderCompact(buf *bytes.Buffer, snaps []DownloadStat) {
	// Show DL/UL header only when downloads are not all finished,
	// matching aria2's RequestGroupMan::downloadFinished() check.
	if !c.opts.DownloadsDone {
		var dlTotal, ulTotal int64
		for i := range snaps {
			dlTotal += snaps[i].Speed
			ulTotal += snaps[i].UploadSpeed
		}
		buf.WriteString(clrMagenta)
		buf.WriteByte('[')
		buf.WriteString(clrClear)
		buf.WriteString("DL:")
		buf.WriteString(clrGreen)
		buf.WriteString(abbrevSize(dlTotal))
		buf.WriteByte('B')
		buf.WriteString(clrClear)
		if ulTotal > 0 {
			buf.WriteString(" UL:")
			buf.WriteString(clrCyan)
			buf.WriteString(abbrevSize(ulTotal))
			buf.WriteByte('B')
			buf.WriteString(clrClear)
		}
		buf.WriteString(clrMagenta)
		buf.WriteByte(']')
		buf.WriteString(clrClear)
	}

	const maxItems = 5
	var scratch [16]byte
	for i := range snaps {
		if i >= maxItems {
			buf.WriteString("(+")
			buf.Write(strconv.AppendInt(scratch[:0], int64(len(snaps)-maxItems), 10))
			buf.WriteString(")")
			break
		}
		buf.WriteString(clrMagenta)
		buf.WriteByte('[')
		buf.WriteString(clrClear)
		s := &snaps[i]
		buf.WriteByte('#')
		buf.WriteString(abbrevGID(s.GID))
		buf.WriteByte(' ')
		c.writeSizeProgress(buf, s)
		buf.WriteString(clrMagenta)
		buf.WriteByte(']')
		buf.WriteString(clrClear)
	}
}

func (c *Console) renderSingle(buf *bytes.Buffer, s *DownloadStat) {
	buf.WriteString(clrMagenta)
	buf.WriteByte('[')
	buf.WriteString(clrClear)
	buf.WriteByte('#')
	buf.WriteString(abbrevGID(s.GID))
	buf.WriteByte(' ')
	c.writeSizeProgress(buf, s)

	var scratch [16]byte
	buf.WriteString(" CN:")
	buf.Write(strconv.AppendInt(scratch[:0], int64(s.Connections), 10))
	if s.NumSeeders > 0 {
		buf.WriteString(" SD:")
		buf.Write(strconv.AppendInt(scratch[:0], int64(s.NumSeeders), 10))
	}
	if s.Status != "complete" {
		buf.WriteString(" DL:")
		buf.WriteString(clrGreen)
		buf.WriteString(abbrevSize(s.Speed))
		buf.WriteByte('B')
		buf.WriteString(clrClear)
	}
	// Show UL only when session upload length > 0, matching aria2's
	// ConsoleStatCalc which checks stat.sessionUploadLength > 0.
	if s.SessionUploadLength > 0 {
		buf.WriteString(" UL:")
		buf.WriteString(clrCyan)
		buf.WriteString(abbrevSize(s.UploadSpeed))
		buf.WriteByte('B')
		buf.WriteString(clrClear)
		// Session upload total in parens, matching:
		//   o << "(" << sizeFormatter(stat.allTimeUploadLength) << "B)"
		buf.WriteByte('(')
		buf.WriteString(abbrevSize(s.AllTimeUploadLength))
		buf.WriteString("B)")
	}
	if s.ETA > 0 {
		buf.WriteString(" ETA:")
		buf.WriteString(clrYellow)
		buf.WriteString(secfmt(s.ETA))
		buf.WriteString(clrClear)
	}
	buf.WriteString(clrMagenta)
	buf.WriteByte(']')
	buf.WriteString(clrClear)
}

func (c *Console) writeSizeProgress(buf *bytes.Buffer, s *DownloadStat) {
	if s.Seeder {
		buf.WriteString("SEED(")
		if s.CompletedSize > 0 {
			// Share ratio = allTimeUploadLength / completedLength, matching aria2's
			// ConsoleStatCalc::printSizeProgress formula:
			//   ((allTimeUploadLength * 10) / completedLength) / 10.0
			ratio := float64(s.AllTimeUploadLength*10/s.CompletedSize) / 10.0
			fmt.Fprintf(buf, "%.1f", ratio)
		} else {
			buf.WriteString("--")
		}
		buf.WriteString(")")
		return
	}
	buf.WriteString(abbrevSize(s.CompletedSize))
	buf.WriteString("B/")
	buf.WriteString(abbrevSize(s.TotalSize))
	buf.WriteString("B")
	if s.TotalSize > 0 {
		pct := 100 * s.CompletedSize / s.TotalSize
		buf.WriteString(clrCyan)
		buf.WriteByte('(')
		var scratch [16]byte
		buf.Write(strconv.AppendInt(scratch[:0], pct, 10))
		buf.WriteString("%)")
		buf.WriteString(clrClear)
	}
}

func (c *Console) renderSummary(snapshots []DownloadStat, cols int) {
	sepEq := strings.Repeat("=", cols)
	now := time.Now()
	dateStr := now.Format("Mon Jan _2 15:04:05 2006")
	fmt.Fprintf(c.out, " *** Download Progress Summary as of %s *** \n%s\n", dateStr, sepEq)

	sepDash := strings.Repeat("-", cols)
	for i := range snapshots {
		buf := bufPool.Get().(*bytes.Buffer)
		buf.Reset()
		c.renderSingle(buf, &snapshots[i])
		c.writeStripped(c.out, buf)
		bufPool.Put(buf)
		if snapshots[i].Filename != "" {
			fmt.Fprintf(c.out, "\nFILE: %s", snapshots[i].Filename)
		}
		fmt.Fprint(c.out, "\n")
		fmt.Fprintln(c.out, sepDash)
	}
	fmt.Fprintln(c.out)
}

func (c *Console) appendAllocProgress(buf *bytes.Buffer, p *AllocProgress) {
	if p == nil {
		return
	}
	buf.WriteString(" [FileAlloc:#")
	buf.WriteString(abbrevGID(p.GID))
	buf.WriteByte(' ')
	buf.WriteString(abbrevSize(p.CurrentSize))
	buf.WriteString("B/")
	buf.WriteString(abbrevSize(p.TotalSize))
	buf.WriteString("B(")
	if p.TotalSize > 0 {
		var scratch [16]byte
		buf.Write(strconv.AppendInt(scratch[:0], 100*p.CurrentSize/p.TotalSize, 10))
	} else {
		buf.WriteString("--")
	}
	buf.WriteString("%)]")
	if p.Queued > 0 {
		var scratch [16]byte
		buf.WriteString("(+")
		buf.Write(strconv.AppendInt(scratch[:0], int64(p.Queued), 10))
		buf.WriteByte(')')
	}
}

func (c *Console) appendCheckProgress(buf *bytes.Buffer, p *CheckProgress) {
	if p == nil {
		return
	}
	buf.WriteString(" [Checksum:#")
	buf.WriteString(abbrevGID(p.GID))
	buf.WriteByte(' ')
	buf.WriteString(abbrevSize(p.CurrentSize))
	buf.WriteString("B/")
	buf.WriteString(abbrevSize(p.TotalSize))
	buf.WriteString("B(")
	if p.TotalSize > 0 {
		var scratch [16]byte
		buf.Write(strconv.AppendInt(scratch[:0], 100*p.CurrentSize/p.TotalSize, 10))
	} else {
		buf.WriteString("--")
	}
	buf.WriteString("%)]")
	if p.Queued > 0 {
		var scratch [16]byte
		buf.WriteString("(+")
		buf.Write(strconv.AppendInt(scratch[:0], int64(p.Queued), 10))
		buf.WriteByte(')')
	}
}

func (c *Console) showReadout() bool {
	if !c.opts.ShowReadoutSet {
		return true
	}
	return c.opts.ShowReadout
}

func (c *Console) truncateReadout() bool {
	if !c.opts.TruncateSet {
		return true
	}
	return c.opts.Truncate
}

// Signals registers SIGINT, SIGTERM, SIGHUP (Unix) or equivalent
// interrupt signals (Windows) and returns a channel that receives them.
// Multi-stage handling: first SIGINT → graceful shutdown; second SIGINT
// or any SIGTERM/SIGHUP → force shutdown. The caller is responsible for
// interpreting the signal sequence.
// Callers should call cancel() on the context to stop signal delivery.
func (c *Console) Signals(ctx context.Context) <-chan os.Signal {
	c.sigCh = make(chan os.Signal, 1)
	c.registerSignals(c.sigCh)
	go func() {
		<-ctx.Done()
		signal.Stop(c.sigCh)
		close(c.sigCh)
	}()
	return c.sigCh
}

// RunInteractive starts an interactive console session that reads
// commands from stdin. Each line (minus trailing whitespace) is
// dispatched to the provided function; the response is printed.
// The loop exits when ctx is cancelled or stdin returns io.EOF.
//
// This is an aria2go extension: aria2 has no stdin command loop.
func (c *Console) RunInteractive(ctx context.Context, dispatch func(cmd string) string) error {
	stdin := os.Stdin
	buf := make([]byte, 4096)
	var line []byte

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, err := stdin.Read(buf)
		if n > 0 {
			line = append(line, buf[:n]...)
			for {
				idx := bytes.IndexByte(line, '\n')
				if idx < 0 {
					break
				}
				cmd := strings.TrimRight(string(line[:idx]), "\r\n\t ")
				line = line[idx+1:]
				if cmd == "" {
					continue
				}
				if cmd == "quit" || cmd == "exit" || cmd == "q" {
					return nil
				}
				resp := dispatch(cmd)
				if resp != "" {
					fmt.Fprintln(c.out, resp)
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

// abbrevGID returns the first 6 hex chars of a GID, matching aria2's
// GroupId::toAbbrevHex.
func abbrevGID(gid string) string {
	if len(gid) >= 6 {
		return gid[:6]
	}
	return gid
}

// abbrevSize formats a byte count in human-readable form, matching
// aria2's util::abbrevSize. Uses binary prefixes with one decimal
// when the integer part is a single digit and a higher unit exists.
func abbrevSize(size int64) string {
	if size < 0 {
		return "0"
	}
	units := []string{"", "Ki", "Mi", "Gi"}
	t := size
	uidx := 0
	r := int64(0)
	for t >= 1024 && uidx+1 < len(units) {
		r = t % 1024
		t /= 1024
		uidx++
	}
	// If within 10% of the next unit boundary, round up.
	if uidx+1 < len(units) && t >= 922 {
		uidx++
		r = t
		t = 0
	}
	var buf [32]byte
	b := buf[:0]
	if t < 10 && uidx > 0 {
		frac := (r * 10) / 1024
		b = append(b, '0'+byte(t))
		b = append(b, '.')
		b = strconv.AppendInt(b, frac, 10)
		b = append(b, units[uidx]...)
		return string(b)
	}
	b = appendCommaInt(b, t)
	b = append(b, units[uidx]...)
	return string(b)
}

// appendCommaInt appends the decimal representation of n with comma
// separators (every 3 orders of magnitude) to buf.
func appendCommaInt(buf []byte, n int64) []byte {
	if n == 0 {
		return append(buf, '0')
	}
	var tmp [20]byte
	b := strconv.AppendInt(tmp[:0], n, 10)
	rem := len(b) % 3
	if rem == 0 {
		rem = 3
	}
	buf = append(buf, b[:rem]...)
	for i := rem; i < len(b); i += 3 {
		buf = append(buf, ',')
		buf = append(buf, b[i:i+3]...)
	}
	return buf
}

// formatInt formats an int64 with commas for every three orders of magnitude.
func formatInt(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [25]byte
	b := appendCommaInt(buf[:0], n)
	return string(b)
}

// secfmt formats a duration in seconds as XhYmZs, matching aria2's
// util::secfmt. Always shows seconds when the value is >= 60 and
// a multiple of 60 (e.g. "1m0s" not "1m").
func secfmt(sec int64) string {
	tsec := sec
	if sec == 0 {
		return "0s"
	}
	var buf [32]byte
	b := buf[:0]
	if h := sec / 3600; h > 0 {
		b = strconv.AppendInt(b, h, 10)
		b = append(b, 'h')
		sec %= 3600
	}
	if m := sec / 60; m > 0 {
		b = strconv.AppendInt(b, m, 10)
		b = append(b, 'm')
		sec %= 60
	}
	if sec > 0 || (tsec >= 60 && tsec%60 == 0) {
		b = strconv.AppendInt(b, sec, 10)
		b = append(b, 's')
	}
	return string(b)
}

// stripColors removes ANSI escape sequences from a string.
func stripColors(s string) string {
	var buf strings.Builder
	buf.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j
				continue
			}
		}
		buf.WriteByte(s[i])
	}
	return buf.String()
}

func truncateANSI(s string, cols int) string {
	if cols <= 0 || len(s) == 0 {
		return ""
	}

	var buf strings.Builder
	buf.Grow(len(s))

	visible := 0
	hasANSI := false
	for i := 0; i < len(s); {
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				hasANSI = true
				buf.WriteString(s[i : j+1])
				i = j + 1
				continue
			}
		}
		if visible >= cols {
			break
		}
		buf.WriteByte(s[i])
		visible++
		i++
	}

	out := buf.String()
	if hasANSI && !strings.HasSuffix(out, clrClear) {
		out += clrClear
	}
	return out
}

// isatty returns true if the output writer is a terminal.
func (c *Console) isatty() bool {
	f, ok := c.out.(*os.File)
	if !ok {
		return false
	}
	return isTerminal(f.Fd())
}
