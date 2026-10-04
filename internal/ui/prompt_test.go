package ui

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// withStdin swaps os.Stdin for the read end of r and resets the shared reader
// state, restoring everything afterwards.
func withStdin(t *testing.T, r *os.File) {
	t.Helper()
	stdinOnce.Do(func() {})
	oldStdin, oldReader, oldPending := os.Stdin, stdinReader, pendingRead
	os.Stdin = r
	stdinReader = bufio.NewReader(r)
	pendingRead = nil
	t.Cleanup(func() {
		os.Stdin, stdinReader, pendingRead = oldStdin, oldReader, oldPending
	})
}

func TestConfirmOpenUnwrittenPipeFailsClosed(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	withStdin(t, r)

	old := stdinAnswerWait
	stdinAnswerWait = 50 * time.Millisecond
	defer func() { stdinAnswerWait = old }()

	done := make(chan error, 1)
	go func() {
		_, err := Confirm("ok?", true)
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, ErrNoAnswer) {
			t.Fatalf("expected ErrNoAnswer, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Confirm blocked on an open pipe nobody writes to")
	}
}

func TestConfirmPipedAnswerStillWorks(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	withStdin(t, r)

	if _, err := io.Copy(w, strings.NewReader("n\n")); err != nil {
		t.Fatal(err)
	}
	w.Close()

	ok, err := Confirm("ok?", true)
	if err != nil || ok {
		t.Fatalf("expected an explicit no, got ok=%v err=%v", ok, err)
	}
}

func TestConfirmAnswerArrivingAfterTimeoutIsKept(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	withStdin(t, r)

	old := stdinAnswerWait
	stdinAnswerWait = 20 * time.Millisecond
	defer func() { stdinAnswerWait = old }()

	if _, err := Confirm("ok?", true); !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("expected ErrNoAnswer, got %v", err)
	}
	if _, err := w.WriteString("n\n"); err != nil {
		t.Fatal(err)
	}
	stdinAnswerWait = 5 * time.Second
	ok, err := Confirm("ok?", true)
	if err != nil || ok {
		t.Fatalf("expected the late answer to be read once, got ok=%v err=%v", ok, err)
	}
}
