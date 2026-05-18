// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
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
	var allowSSH bool
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the local CodeDojo web UI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd.Context(), cmd, serveOptions{
				Repo:       repoPath,
				Port:       port,
				Difficulty: difficulty,
				Budget:     budget,
				AllowSSH:   allowSSH,
			})
		},
	}
	cmd.Flags().StringVar(&repoPath, "repo", "", "default local path or remote URL to prefill in the web UI")
	cmd.Flags().IntVar(&port, "port", 0, "port for the local web server; 0 picks an available port")
	cmd.Flags().IntVar(&difficulty, "difficulty", 0, "default task difficulty from 1 to 5")
	cmd.Flags().IntVar(&budget, "budget", 0, "default hint count budget")
	cmd.Flags().BoolVar(&allowSSH, "allow-ssh", false, "allow the web UI to clone SSH repositories using discovered local SSH keys")
	return cmd
}

type serveOptions struct {
	Repo       string
	Port       int
	Difficulty int
	Budget     int
	AllowSSH   bool
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
	service.SetSSHAuthAllowed(opts.AllowSSH)
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
		Handler:           localRequestGuard(withDefaultRepo(handler, opts.Repo), listener.Addr().String()),
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

func localRequestGuard(next http.Handler, listenAddr string) http.Handler {
	_, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		port = ""
	}
	allowedHosts := map[string]bool{
		listenAddr: true,
	}
	if port != "" {
		allowedHosts["127.0.0.1:"+port] = true
		allowedHosts["localhost:"+port] = true
	}
	allowedOrigins := map[string]bool{}
	for host := range allowedHosts {
		allowedOrigins["http://"+host] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedHosts[r.Host] {
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		if !sameLocalOrigin(r.Header.Get("Origin"), allowedOrigins) ||
			!sameLocalOrigin(refererOrigin(r.Header.Get("Referer")), allowedOrigins) {
			http.Error(w, "invalid origin", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func sameLocalOrigin(origin string, allowed map[string]bool) bool {
	if origin == "" {
		return true
	}
	return allowed[origin]
}

func refererOrigin(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "invalid"
	}
	return parsed.Scheme + "://" + parsed.Host
}
