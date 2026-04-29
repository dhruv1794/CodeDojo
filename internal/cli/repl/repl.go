package repl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

var ErrExit = errors.New("repl exit")

type Handler func(ctx context.Context, line string) error

type LineSource func() (string, bool, error)

type MultilineHandler func(ctx context.Context, line string, next LineSource) error

type Runner struct {
	In        io.Reader
	Out       io.Writer
	Prompt    string
	Handler   Handler
	Multiline MultilineHandler
}

func (r Runner) Run(ctx context.Context) error {
	if r.Handler == nil && r.Multiline == nil {
		return fmt.Errorf("handler is required")
	}
	if r.In == nil {
		r.In = os.Stdin
	}
	if r.Out == nil {
		r.Out = os.Stdout
	}
	if r.Prompt == "" {
		r.Prompt = "> "
	}
	if file, ok := r.In.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		return r.runTerminal(ctx, file)
	}
	return r.runScanner(ctx)
}

func (r Runner) runTerminal(ctx context.Context, file *os.File) error {
	t := term.NewTerminal(readWriter{Reader: file, Writer: r.Out}, r.Prompt)
	next := LineSource(func() (string, bool, error) {
		line, err := t.ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return "", false, nil
			}
			return "", false, err
		}
		return line, true, nil
	})
	for {
		line, ok, err := next()
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if err := r.handle(ctx, line, next); err != nil {
			return err
		}
	}
}

func (r Runner) runScanner(ctx context.Context) error {
	scanner := bufio.NewScanner(r.In)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	next := LineSource(func() (string, bool, error) {
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", false, err
			}
			return "", false, nil
		}
		return scanner.Text(), true, nil
	})
	for {
		line, ok, err := next()
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if _, err := fmt.Fprintf(r.Out, "%s%s\n", r.Prompt, line); err != nil {
			return err
		}
		if err := r.handle(ctx, line, next); err != nil {
			return err
		}
	}
}

func (r Runner) handle(ctx context.Context, line string, next LineSource) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var err error
	if r.Multiline != nil {
		err = r.Multiline(ctx, strings.TrimSpace(line), next)
	} else {
		err = r.Handler(ctx, strings.TrimSpace(line))
	}
	if errors.Is(err, ErrExit) {
		return nil
	}
	return err
}

type readWriter struct {
	io.Reader
	io.Writer
}
