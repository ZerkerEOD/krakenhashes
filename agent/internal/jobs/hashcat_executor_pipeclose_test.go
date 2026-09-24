package jobs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"testing"
)

/*
A chunk that finishes normally must not be reported as failed.

os/exec closes the stdout pipe as soon as Cmd.Wait() sees the process exit, while
the reader goroutine may still be in Scan(). The reader then sees os.ErrClosed.
Reporting that as a task failure cost a production deployment three jobs, killed
at 21%, 22% and 40% — the backend counts task failures against a per-tuple
benchmark cap and fails the whole job at ten.
*/

func TestIsClosedPipeAfterExit(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		// The exact shape os/exec produces: *fs.PathError wrapping os.ErrClosed.
		// Its Error() is "read |0: file already closed" — the string seen in the
		// production log that started this.
		{
			name: "os/exec closed pipe",
			err:  &fs.PathError{Op: "read", Path: "|0", Err: os.ErrClosed},
			want: true,
		},
		{name: "bare sentinel", err: os.ErrClosed, want: true},
		{name: "wrapped sentinel", err: fmt.Errorf("scanner: %w", os.ErrClosed), want: true},
		// Fallback for a future wrapping that loses errors.Is compatibility.
		{name: "text only, sentinel lost", err: errors.New("read |0: file already closed"), want: true},

		{name: "nil", err: nil, want: false},
		// Real faults must still be reported, or we would swallow the very
		// failures this reader exists to surface.
		{name: "unexpected EOF", err: io.ErrUnexpectedEOF, want: false},
		{name: "token too long", err: errors.New("bufio.Scanner: token too long"), want: false},
		{name: "broken pipe", err: errors.New("write |1: broken pipe"), want: false},
		{name: "permission denied", err: os.ErrPermission, want: false},
		{name: "generic io error", err: errors.New("input/output error"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isClosedPipeAfterExit(tt.err); got != tt.want {
				t.Errorf("isClosedPipeAfterExit(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// Proves the premise rather than assuming it: reading a StdoutPipe after Wait
// really does yield an error this classifier recognises. If os/exec ever changed
// that, the guard would silently stop matching and jobs would start dying again.
func TestStdoutPipeAfterWaitIsRecognised(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo hello")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	// Wait has now closed the pipe. This is what the reader goroutine hits when
	// it loses the race.
	_, readErr := pipe.Read(make([]byte, 16))
	if readErr == nil {
		t.Skip("read after Wait succeeded on this platform; nothing to classify")
	}
	if !isClosedPipeAfterExit(readErr) {
		t.Errorf("read-after-Wait error %q is not recognised as the benign close race; "+
			"the stdout reader would report a completed chunk as failed", readErr)
	}
}
