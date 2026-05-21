// Package hookrunner provides fire-and-forget hook command execution matching
// aria2's executeHook(). Hooks are spawned via fork()+execlp() (Unix) or
// CreateProcess (Windows) as detached child processes; Run() returns immediately
// without waiting for exit. GID, file count, and first file path are passed as
// positional arguments matching aria2's call signature.
package hookrunner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
)

var parentEnv []string

func init() {
	parentEnv = os.Environ()
}

// Run spawns command with args as a detached fire-and-forget child process.
// It returns immediately without waiting for exit, matching aria2's
// fork()+execlp() behavior. An error is returned only if the process cannot
// be started (fork/exec failure).
//
// aria2's executeHook() passes GID (hex), numFiles (decimal), and
// firstFilename as positional arguments:
//
//	Run(ctx, command, gidHex, numFilesStr, firstFilename)
func Run(ctx context.Context, command string, args ...string) error {
	logHook(command, args)

	cmd := exec.Command(command, args...)
	cmd.Env = parentEnv
	if err := cmd.Start(); err != nil {
		slog.Error("fork() failed. Cannot execute user command.", "error", err)
		return err
	}
	// Detach: Wait() in a goroutine prevents zombie child processes on Unix.
	go func() {
		_ = cmd.Wait()
	}()
	return nil
}

// Close is a no-op; child processes are fire-and-forget and not tracked.
func Close() error {
	return nil
}

func logHook(command string, args []string) {
	formatStr := "Executing user command: %s"
	logArgs := []any{command}
	for _, a := range args {
		formatStr += " %s"
		logArgs = append(logArgs, a)
	}
	slog.Info(fmt.Sprintf(formatStr, logArgs...))
}
