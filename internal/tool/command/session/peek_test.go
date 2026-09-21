package session

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func newPeekSession() *Session {
	return &Session{ID: "peek-session", StartedAt: time.Now(), Done: make(chan struct{})}
}

func writePeek(t *testing.T, s *Session, stderr bool, data []byte) {
	t.Helper()
	if _, err := (sessionOutputWriter{session: s, stderr: stderr}).Write(data); err != nil {
		t.Fatalf("write output: %v", err)
	}
}

func cursors(s *Session) (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stdoutCursor, s.stderrCursor
}

func TestReadOutputAtRepeatsTheSameRangeWithoutMovingSharedCursors(t *testing.T) {
	s := newPeekSession()
	writePeek(t, s, false, []byte("hello"))
	writePeek(t, s, true, []byte("err"))
	beforeOut, beforeErr := cursors(s)

	first, err := s.ReadOutputAt(0, 0, 64)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ReadOutputAt(0, 0, 64)
	if err != nil {
		t.Fatal(err)
	}
	afterOut, afterErr := cursors(s)
	if afterOut != beforeOut || afterErr != beforeErr {
		t.Fatalf("shared cursors changed from %d/%d to %d/%d", beforeOut, beforeErr, afterOut, afterErr)
	}
	if first.Stdout.Text != "hello" || second.Stdout.Text != first.Stdout.Text || first.Stdout.NextOffset != second.Stdout.NextOffset {
		t.Fatalf("stdout reads diverged: %#v %#v", first.Stdout, second.Stdout)
	}
	if first.Stderr.Text != "err" || second.Stderr.Text != "err" || first.Status != "running" {
		t.Fatalf("stderr/status = %#v", second)
	}
	if first.Completed || first.CommandOK {
		t.Fatal("running peek reported a completed command")
	}
	if first.Stdout.NextOffset != 5 || first.Stderr.NextOffset != 3 || first.Stdout.Gap || first.Stdout.Truncated {
		t.Fatalf("offsets = stdout:%#v stderr:%#v", first.Stdout, first.Stderr)
	}

	snapshot := s.Snapshot("running", 64)
	if snapshot.Stdout != "hello" {
		t.Fatalf("status cursor did not still observe the original output: %q", snapshot.Stdout)
	}
	movedOut, _ := cursors(s)
	if movedOut != len("hello") {
		t.Fatalf("status cursor = %d, want %d", movedOut, len("hello"))
	}
	again, err := s.ReadOutputAt(0, 0, 64)
	if err != nil {
		t.Fatal(err)
	}
	if again.Stdout.Text != "hello" || again.Stdout.NextOffset != 5 {
		t.Fatalf("absolute peek followed the shared cursor: %#v", again.Stdout)
	}
}

func TestReadOutputAtKeepsIndependentObserverOffsets(t *testing.T) {
	s := newPeekSession()
	writePeek(t, s, false, []byte("0123456789"))

	a, err := s.ReadOutputAt(0, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.ReadOutputAt(0, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if a.Stdout.Text != "0123" || b.Stdout.Text != "0123" {
		t.Fatalf("observers stole output: %q %q", a.Stdout.Text, b.Stdout.Text)
	}
	aNext, err := s.ReadOutputAt(a.Stdout.NextOffset, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	bNext, err := s.ReadOutputAt(2, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if aNext.Stdout.Text != "4567" || aNext.Stdout.NextOffset != 8 {
		t.Fatalf("observer A = %#v", aNext.Stdout)
	}
	if bNext.Stdout.Text != "2345" || bNext.Stdout.NextOffset != 6 {
		t.Fatalf("observer B = %#v", bNext.Stdout)
	}
	outCursor, errCursor := cursors(s)
	if outCursor != 0 || errCursor != 0 {
		t.Fatalf("shared cursors = %d/%d, want 0/0", outCursor, errCursor)
	}
}

func TestReadOutputAtPagesRetainedBytesWithoutSkippingToTotal(t *testing.T) {
	s := newPeekSession()
	payload := strings.Repeat("0123456789", 5)
	writePeek(t, s, false, []byte(payload))

	var got strings.Builder
	offset := int64(0)
	for page := 0; page < 20; page++ {
		read, err := s.ReadOutputAt(offset, 0, 8)
		if err != nil {
			t.Fatal(err)
		}
		if read.Stdout.Gap || read.Stdout.PendingUTF8 || read.Stdout.InvalidUTF8 {
			t.Fatalf("page %d flags = %#v", page, read.Stdout)
		}
		if read.Stdout.Truncated && read.Stdout.NextOffset == read.Stdout.TotalBytes {
			t.Fatalf("page %d skipped to total: %#v", page, read.Stdout)
		}
		if read.Stdout.NextOffset < offset || (read.Stdout.Text == "" && read.Stdout.NextOffset == offset && offset != read.Stdout.TotalBytes) {
			t.Fatalf("page %d made no progress from %d: %#v", page, offset, read.Stdout)
		}
		got.WriteString(read.Stdout.Text)
		offset = read.Stdout.NextOffset
		if offset == read.Stdout.TotalBytes {
			if read.Stdout.Truncated {
				t.Fatalf("final page still truncated: %#v", read.Stdout)
			}
			break
		}
	}
	if got.String() != payload || offset != int64(len(payload)) {
		t.Fatalf("reassembled %q ending at %d, want %q", got.String(), offset, payload)
	}
}

func TestReadOutputAtReportsEvictedBytesAsAGap(t *testing.T) {
	s := newPeekSession()
	const limit = 4 * 1024 * 1024
	writePeek(t, s, false, bytesRepeat('x', limit+50))
	writePeek(t, s, false, []byte("END"))

	read, err := s.ReadOutputAt(0, 0, limit)
	if err != nil {
		t.Fatal(err)
	}
	if !read.Stdout.Gap {
		t.Fatal("offset 0 hid evicted bytes")
	}
	if read.Stdout.RetainedFrom != 53 || read.Stdout.TotalBytes != int64(limit+53) {
		t.Fatalf("range = retained:%d total:%d", read.Stdout.RetainedFrom, read.Stdout.TotalBytes)
	}
	if !strings.HasSuffix(read.Stdout.Text, "END") || strings.Contains(read.Stdout.Text, "LOST") {
		t.Fatalf("retained text does not end at the live suffix: tail %q", tail(read.Stdout.Text, 8))
	}
	if read.Stdout.NextOffset != read.Stdout.TotalBytes || read.Stdout.Truncated {
		t.Fatalf("full retained read = %#v", read.Stdout)
	}

	atRetained, err := s.ReadOutputAt(read.Stdout.RetainedFrom, 0, limit)
	if err != nil {
		t.Fatal(err)
	}
	if atRetained.Stdout.Gap || atRetained.Stdout.Text != read.Stdout.Text {
		t.Fatalf("read at retained start = %#v", atRetained.Stdout)
	}
	before, err := s.ReadOutputAt(read.Stdout.RetainedFrom-1, 0, 16)
	if err != nil {
		t.Fatal(err)
	}
	if !before.Stdout.Gap || before.Stdout.NextOffset <= read.Stdout.RetainedFrom-1 {
		t.Fatalf("offset inside the gap did not advance into retained bytes: %#v", before.Stdout)
	}
}

func TestReadOutputAtPreservesUTF8OffsetsAndPendingTails(t *testing.T) {
	s := newPeekSession()
	syllable := []byte("안")
	if len(syllable) != 3 {
		t.Fatalf("fixture syllable length = %d", len(syllable))
	}
	writePeek(t, s, false, syllable[:1])
	pending, err := s.ReadOutputAt(0, 0, 64)
	if err != nil {
		t.Fatal(err)
	}
	if !pending.Stdout.PendingUTF8 || pending.Stdout.Text != "" || pending.Stdout.NextOffset != 0 || pending.Stdout.InvalidUTF8 {
		t.Fatalf("incomplete running tail = %#v", pending.Stdout)
	}

	writePeek(t, s, false, syllable[1:])
	emoji := []byte("😀")
	writePeek(t, s, false, emoji[:2])
	partial, err := s.ReadOutputAt(0, 0, 64)
	if err != nil {
		t.Fatal(err)
	}
	if partial.Stdout.Text != "안" || partial.Stdout.NextOffset != 3 || !partial.Stdout.PendingUTF8 || !utf8.ValidString(partial.Stdout.Text) {
		t.Fatalf("fragmented read = %#v", partial.Stdout)
	}
	writePeek(t, s, false, emoji[2:])
	done, err := s.ReadOutputAt(partial.Stdout.NextOffset, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if done.Stdout.Text != "😀" || done.Stdout.NextOffset != int64(len(syllable)+len(emoji)) || done.Stdout.PendingUTF8 || done.Stdout.Truncated {
		t.Fatalf("emoji read = %#v", done.Stdout)
	}
}

func TestReadOutputAtReplacesInvalidBytesAndCompletedTails(t *testing.T) {
	s := newPeekSession()
	writePeek(t, s, false, []byte{'a', 0xff, 'b'})
	replaced, err := s.ReadOutputAt(0, 0, 64)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Stdout.Text != "a\uFFFDb" || !replaced.Stdout.InvalidUTF8 || replaced.Stdout.NextOffset != 3 || !utf8.ValidString(replaced.Stdout.Text) {
		t.Fatalf("invalid byte read = %#v", replaced.Stdout)
	}

	mid := newPeekSession()
	writePeek(t, mid, false, []byte("한글"))
	fromMiddle, err := mid.ReadOutputAt(1, 0, 4)
	if err != nil {
		t.Fatal(err)
	}
	if fromMiddle.Stdout.NextOffset <= 1 || !fromMiddle.Stdout.InvalidUTF8 || !utf8.ValidString(fromMiddle.Stdout.Text) {
		t.Fatalf("mid-code-point read made no progress: %#v", fromMiddle.Stdout)
	}
	if len(fromMiddle.Stdout.Text) > 4 {
		t.Fatalf("display budget exceeded: %q", fromMiddle.Stdout.Text)
	}

	finished := newPeekSession()
	finished.completed = true
	finished.exitCode = 0
	finished.FinishedAt = time.Now()
	writePeek(t, finished, false, []byte{0xED, 0x95})
	tailRead, err := finished.ReadOutputAt(0, 0, 64)
	if err != nil {
		t.Fatal(err)
	}
	if tailRead.Stdout.PendingUTF8 || !tailRead.Stdout.InvalidUTF8 || tailRead.Stdout.NextOffset != 2 || tailRead.Status != "exited" || !tailRead.CommandOK {
		t.Fatalf("completed tail = %#v", tailRead)
	}
	if tailRead.Stdout.Text != "\uFFFD\uFFFD" {
		t.Fatalf("completed tail text = %q", tailRead.Stdout.Text)
	}

	if _, err := s.ReadOutputAt(99, 0, 64); err == nil {
		t.Fatal("future offset was accepted")
	}
}

func bytesRepeat(value byte, count int) []byte {
	out := make([]byte, count)
	for i := range out {
		out[i] = value
	}
	return out
}
