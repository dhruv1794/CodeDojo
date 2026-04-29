package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/dhruvmishra/codedojo/internal/app"
	"github.com/dhruvmishra/codedojo/internal/config"
	"github.com/dhruvmishra/codedojo/internal/web"
	"github.com/spf13/cobra"
)

func newServeCommand() *cobra.Command {
	var repoPath string
	var port int
	var difficulty int
	var budget int
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the local CodeDojo web UI",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(cmd.Context(), cmd, serveOptions{
				Repo:       repoPath,
				Port:       port,
				Difficulty: difficulty,
				Budget:     budget,
			})
		},
	}
	cmd.Flags().StringVar(&repoPath, "repo", "", "default local path or remote URL to prefill in the web UI")
	cmd.Flags().IntVar(&port, "port", 0, "port for the local web server; 0 picks an available port")
	cmd.Flags().IntVar(&difficulty, "difficulty", 0, "default task difficulty from 1 to 5")
	cmd.Flags().IntVar(&budget, "budget", 0, "default hint count budget")
	return cmd
}

type serveOptions struct {
	Repo       string
	Port       int
	Difficulty int
	Budget     int
}

func runServe(ctx context.Context, cmd *cobra.Command, opts serveOptions) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return err
	}
	if opts.Difficulty > 0 {
		cfg.Defaults.Difficulty = opts.Difficulty
	}
	if opts.Budget > 0 {
		cfg.Defaults.HintBudget = opts.Budget
	}

	selected := selectSandbox(ctx, cmd.ErrOrStderr())
	service, err := app.NewService(ctx, cfg, selected.driver, selected.spec)
	if err != nil {
		return err
	}
	defer service.Close()

	handler, err := web.New(service)
	if err != nil {
		return err
	}
	addr := fmt.Sprintf("127.0.0.1:%d", opts.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	defer listener.Close()

	server := &http.Server{
		Handler:           withDefaultRepo(handler, opts.Repo),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()

	url := "http://" + listener.Addr().String()
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "CodeDojo web UI: %s\n", url); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return ctx.Err()
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func withDefaultRepo(next http.Handler, repoPath string) http.Handler {
	if repoPath == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.SetCookie(w, &http.Cookie{
				Name:     "codedojo_repo",
				Value:    repoPath,
				Path:     "/",
				SameSite: http.SameSiteLaxMode,
			})
		}
		next.ServeHTTP(w, r)
	})
}
