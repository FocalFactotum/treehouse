package ui

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// ErrNoAnswer reports that a prompt on a non-terminal stdin received no answer
// within stdinAnswerWait. Callers treat it like EOF: the prompt is unanswered.
var ErrNoAnswer = errors.New("no answer on stdin")

// stdinAnswerWait bounds how long a prompt waits for an answer on a stdin that
// is not a terminal. A scripted answer is already in the pipe or file when the
// prompt reads it; an open pipe that nobody writes to (the usual state under CI
// steps and agent harnesses) never delivers one, and would block forever.
var stdinAnswerWait = 3 * time.Second

type lineResult struct {
	line string
	err  error
}

// pendingRead is a read still in flight after its prompt gave up, so a later
// prompt waits on that read instead of starting a second one on the same
// reader.
var pendingRead chan lineResult

var (
	stdinOnce   sync.Once
	stdinReader *bufio.Reader
)

// promptReader is one reader for the whole process. bufio pulls a whole chunk
// off the file descriptor, so a reader built per prompt discards every byte
// that arrived in the same read as the answer it consumed: piped answers after
// the first were lost and read back as EOF. A terminal hid it, because
// canonical mode delivers exactly one line per read.
func promptReader() *bufio.Reader {
	stdinOnce.Do(func() {
		stdinReader = bufio.NewReader(os.Stdin)
	})
	return stdinReader
}

func Confirm(message string, defaultYes bool) (bool, error) {
	hint := "Y/n"
	if !defaultYes {
		hint = "y/N"
	}

	fmt.Fprintf(os.Stderr, "%s [%s] ", message, hint)

	// ReadString reports io.EOF together with whatever it had already read, so
	// a final answer that arrives without a trailing newline comes back as a
	// non-empty line AND an error. Returning early on the error alone discards
	// an answer the operator actually gave: the prompt reads as unanswered, the
	// worktree stays held, and the command reports an abort over an explicit
	// confirmation. The read is therefore normalized FIRST and judged on the
	// same value the interpretation below uses: an errored read that carried
	// only whitespace holds no answer, so it stays unanswered instead of
	// selecting the default. A blank line with no error is the bare Enter the
	// [Y/n] hint documents, and still selects the default.
	line, err := readLine()
	input := strings.TrimSpace(strings.ToLower(line))
	if err != nil && input == "" {
		return defaultYes, err
	}

	if input == "" {
		return defaultYes, nil
	}

	return input == "y" || input == "yes", nil
}

// readLine reads one line from stdin. A terminal blocks until the operator
// answers. Any other stdin is given stdinAnswerWait to deliver the line and is
// otherwise reported as ErrNoAnswer.
func readLine() (string, error) {
	if stdinIsTerminal() {
		return promptReader().ReadString('\n')
	}
	if pendingRead == nil {
		ch := make(chan lineResult, 1)
		pendingRead = ch
		go func() {
			line, err := promptReader().ReadString('\n')
			ch <- lineResult{line, err}
		}()
	}
	timer := time.NewTimer(stdinAnswerWait)
	defer timer.Stop()
	select {
	case r := <-pendingRead:
		pendingRead = nil
		return r.line, r.err
	case <-timer.C:
		return "", ErrNoAnswer
	}
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
