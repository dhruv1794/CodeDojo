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

type Runner struct {
	In      io.Reader
	Out     io.Writer
	Prompt  string
	Handler Handler
}

func (r Runner) Run(ctx context.Context) error {
	if r.Handler == nil {
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
	for {
		line, err := t.ReadLine()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if err := r.handle(ctx, line); err != nil {
			return err
		}
	}
}

func (r Runner) runScanner(ctx context.Context) error {
	scanner := bufio.NewScanner(r.In)
	for {
		if _, err := fmt.Fprint(r.Out, r.Prompt); err != nil {
			return err
		}
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return err
			}
			return nil
		}
		if err := r.handle(ctx, scanner.Text()); err != nil {
			return err
		}
	}
}

func (r Runner) handle(ctx context.Context, line string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := r.Handler(ctx, strings.TrimSpace(line))
	if errors.Is(err, ErrExit) {
		return nil
	}
	return err
}

type readWriter struct {
	io.Reader
	io.Writer
}
