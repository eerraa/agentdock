package session

import (
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/uvwt/agentdock/internal/activity"
)

// MaxSafeOutputOffset is the largest absolute byte offset a JSON client can
// address without rounding through an IEEE-754 number (2^53-1).
const MaxSafeOutputOffset int64 = 9007199254740991

// OutputCursorError reports an absolute output offset that is outside the stream.
type OutputCursorError struct {
	Stream string
	Offset int64
	Total  int64
}

func (e *OutputCursorError) Error() string {
	if e == nil {
		return "output offset is outside the stream"
	}
	return fmt.Sprintf("%s output offset %d is outside the stream ending at %d", e.Stream, e.Offset, e.Total)
}

// StreamWindow is one non-consuming absolute read of a retained output stream.
type StreamWindow struct {
	Text         string
	NextOffset   int64
	RetainedFrom int64
	TotalBytes   int64
	Gap          bool
	Truncated    bool
	PendingUTF8  bool
	InvalidUTF8  bool
}

// OutputAt is a point-in-time read of both streams. It does not move the shared
// status cursors used by session status and stdin writes.
type OutputAt struct {
	activity.Binding
	ActivityWarning string
	SessionID       string
	Status          string
	ElapsedMS       int64
	TimedOut        bool
	Terminal        string
	Completed       bool
	ExitCode        int
	CommandOK       bool
	CommandError    string
	Runtime         string
	WSLDistribution string
	Workdir         string
	Stdout          StreamWindow
	Stderr          StreamWindow
}

// ReadOutputAt copies retained stdout and stderr around absolute byte offsets.
// Callers keep their own offsets. The shared session cursors are left unchanged.
func (s *Session) ReadOutputAt(stdoutOffset, stderrOffset int64, maxBytes int) (OutputAt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	stdout, err := readStreamWindow("stdout", s.stdout.Bytes(), int64(s.stdoutDroppedBytes), int64(s.stdoutTotalBytes), stdoutOffset, maxBytes, s.completed)
	if err != nil {
		return OutputAt{}, err
	}
	stderr, err := readStreamWindow("stderr", s.stderr.Bytes(), int64(s.stderrDroppedBytes), int64(s.stderrTotalBytes), stderrOffset, maxBytes, s.completed)
	if err != nil {
		return OutputAt{}, err
	}

	finished := time.Now()
	status := "running"
	if s.completed {
		finished = s.FinishedAt
		status = "exited"
		if s.TimedOut {
			status = "timeout"
		}
	}
	commandError := ""
	if s.completed && s.waitErr != nil {
		commandError = s.waitErr.Error()
	}
	return OutputAt{
		Binding:         s.activityBinding,
		ActivityWarning: s.activityWarning,
		SessionID:       s.ID,
		Status:          status,
		ElapsedMS:       finished.Sub(s.StartedAt).Milliseconds(),
		TimedOut:        s.TimedOut,
		Terminal:        s.Terminal,
		Completed:       s.completed,
		ExitCode:        s.exitCode,
		CommandOK:       s.completed && s.exitCode == 0 && !s.TimedOut,
		CommandError:    commandError,
		Runtime:         s.execution.Runtime,
		WSLDistribution: s.execution.Distribution,
		Workdir:         s.execution.Workdir,
		Stdout:          stdout,
		Stderr:          stderr,
	}, nil
}

// readStreamWindow applies the absolute-offset read to one retained buffer.
// A is the requested offset, R is retainedFrom, and T is total.
// A > T is rejected. The returned span starts at max(A, R) and stops on the
// display-byte budget, without skipping ahead to T.
func readStreamWindow(stream string, raw []byte, retainedFrom, total, offset int64, budget int, completed bool) (StreamWindow, error) {
	if offset < 0 || offset > MaxSafeOutputOffset || offset > total {
		return StreamWindow{}, &OutputCursorError{Stream: stream, Offset: offset, Total: total}
	}
	start := offset
	gap := offset < retainedFrom
	if start < retainedFrom {
		start = retainedFrom
	}
	relative := start - retainedFrom
	if relative < 0 || relative > int64(len(raw)) {
		return StreamWindow{}, &OutputCursorError{Stream: stream, Offset: offset, Total: total}
	}
	text, consumed, invalid, pending, truncated := decodeDisplayPrefix(raw[int(relative):], budget, completed)
	next := start + int64(consumed)
	if next > total {
		next = total
	}
	return StreamWindow{
		Text:         text,
		NextOffset:   next,
		RetainedFrom: retainedFrom,
		TotalBytes:   total,
		Gap:          gap,
		Truncated:    truncated,
		PendingUTF8:  pending,
		InvalidUTF8:  invalid,
	}, nil
}

// decodeDisplayPrefix returns a UTF-8 display prefix whose encoded size is at most budget.
// next-offset accounting is the caller's job and uses the returned raw byte count.
func decodeDisplayPrefix(raw []byte, budget int, completed bool) (text string, consumed int, invalid, pending, truncated bool) {
	var builder []byte
	for consumed < len(raw) {
		rest := raw[consumed:]
		if !utf8.FullRune(rest) {
			if !completed {
				pending = true
				return string(builder), consumed, invalid, pending, false
			}
			for consumed < len(raw) {
				if !hasDisplayRoom(len(builder), utf8.RuneLen(utf8.RuneError), budget) {
					return string(builder), consumed, invalid, false, true
				}
				builder = utf8.AppendRune(builder, utf8.RuneError)
				consumed++
				invalid = true
			}
			return string(builder), consumed, invalid, false, false
		}
		runeValue, size := utf8.DecodeRune(rest)
		if runeValue == utf8.RuneError && size == 1 {
			replacementLen := utf8.RuneLen(utf8.RuneError)
			if !hasDisplayRoom(len(builder), replacementLen, budget) {
				return string(builder), consumed, invalid, false, true
			}
			builder = utf8.AppendRune(builder, utf8.RuneError)
			consumed++
			invalid = true
			continue
		}
		if !hasDisplayRoom(len(builder), size, budget) {
			return string(builder), consumed, invalid, false, true
		}
		builder = append(builder, rest[:size]...)
		consumed += size
	}
	return string(builder), consumed, invalid, false, false
}

func hasDisplayRoom(used, extra, budget int) bool {
	if extra <= 0 || budget < 0 || used < 0 || used > budget {
		return false
	}
	return extra <= budget-used
}
