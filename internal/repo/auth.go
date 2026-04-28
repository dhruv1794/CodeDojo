package repo

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	gitssh "github.com/go-git/go-git/v5/plumbing/transport/ssh"
)

type AuthHints struct {
	GitHubToken string
	GitAskPass  string
	SSHKeys     []string
}

func EnvAuthHints() AuthHints {
	return AuthHints{
		GitHubToken: os.Getenv("GITHUB_TOKEN"),
		GitAskPass:  os.Getenv("GIT_ASKPASS"),
		SSHKeys:     discoverSSHKeys(),
	}
}

func authForURL(rawURL string) (transport.AuthMethod, error) {
	return authForURLWithHints(rawURL, EnvAuthHints())
}

func authForURLWithHints(rawURL string, hints AuthHints) (transport.AuthMethod, error) {
	switch {
	case isHTTPURL(rawURL):
		return httpAuth(rawURL, hints)
	case isSSHURL(rawURL):
		return sshAuth(hints)
	default:
		return nil, nil
	}
}

func httpAuth(rawURL string, hints AuthHints) (transport.AuthMethod, error) {
	if hints.GitHubToken != "" {
		return &githttp.BasicAuth{Username: "x-access-token", Password: hints.GitHubToken}, nil
	}
	if hints.GitAskPass == "" {
		return nil, nil
	}
	username, err := runAskPass(hints.GitAskPass, fmt.Sprintf("Username for %q:", rawURL))
	if err != nil {
		return nil, err
	}
	password, err := runAskPass(hints.GitAskPass, fmt.Sprintf("Password for %q:", rawURL))
	if err != nil {
		return nil, err
	}
	if username == "" {
		username = "git"
	}
	if password == "" {
		return nil, nil
	}
	return &githttp.BasicAuth{Username: username, Password: password}, nil
}

func sshAuth(hints AuthHints) (transport.AuthMethod, error) {
	for _, keyPath := range hints.SSHKeys {
		auth, err := gitssh.NewPublicKeysFromFile("git", keyPath, "")
		if err == nil {
			return auth, nil
		}
	}
	return nil, nil
}

func runAskPass(path, prompt string) (string, error) {
	out, err := exec.Command(path, prompt).Output()
	if err != nil {
		return "", fmt.Errorf("run GIT_ASKPASS %q: %w", path, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func discoverSSHKeys() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(home, ".ssh", "id_*"))
	if err != nil {
		return nil
	}
	keys := make([]string, 0, len(matches))
	for _, match := range matches {
		if strings.HasSuffix(match, ".pub") {
			continue
		}
		info, err := os.Stat(match)
		if err != nil || info.IsDir() {
			continue
		}
		keys = append(keys, match)
	}
	sort.Strings(keys)
	return keys
}

func isHTTPURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

func isSSHURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err == nil && parsed.Scheme == "ssh" {
		return true
	}
	return strings.Contains(rawURL, "@") && strings.Contains(rawURL, ":") && !strings.Contains(rawURL, "://")
}
