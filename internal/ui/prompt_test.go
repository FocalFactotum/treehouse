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
	oldStdin, oldReader, oldExhausted := os.Stdin, stdinReader, stdinExhausted
	os.Stdin = r
	stdinReader = bufio.NewReader(r)
	stdinExhausted = false
	t.Cleanup(func() {
		os.Stdin, stdinReader, stdinExhausted = oldStdin, oldReader, oldExhausted
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

	if _, err := io.Copy(w, strings.NewReader("n\ny\nyes")); err != nil {
		t.Fatal(err)
	}
	w.Close()

	ok, err := Confirm("ok?", true)
	if err != nil || ok {
		t.Fatalf("expected an explicit no, got ok=%v err=%v", ok, err)
	}
	for i := 0; i < 2; i++ {
		ok, err := Confirm("ok?", false)
		if err != nil || !ok {
			t.Fatalf("expected an explicit yes, got ok=%v err=%v", ok, err)
		}
	}
}

func TestConfirmAnswerArrivingAfterTimeoutIsDiscarded(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	withStdin(t, r)

	old := stdinAnswerWait
	stdinAnswerWait = 20 * time.Millisecond
	defer func() { stdinAnswerWait = old }()

	if _, err := Confirm("ok?", true); !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("expected ErrNoAnswer, got %v", err)
	}
	if _, err := w.WriteString("y\ny\n"); err != nil {
		t.Fatal(err)
	}
	stdinAnswerWait = 5 * time.Second
	for _, defaultYes := range []bool{true, false} {
		start := time.Now()
		_, err := Confirm("ok?", defaultYes)
		if !errors.Is(err, ErrNoAnswer) {
			t.Fatalf("expected late answers discarded, got %v", err)
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("exhausted stdin waited %v instead of failing immediately", elapsed)
		}
	}
}
