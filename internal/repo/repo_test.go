package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
)

func TestCloneLocalFixture(t *testing.T) {
	src := newGitFixture(t)
	dest := filepath.Join(t.TempDir(), "clone")

	cloned, err := Clone(context.Background(), src, dest)
	if err != nil {
		t.Fatalf("Clone() error = %v", err)
	}
	if cloned.Path != dest {
		t.Fatalf("Clone() path = %q, want %q", cloned.Path, dest)
	}
	if cloned.Git == nil {
		t.Fatalf("Clone() Git is nil")
	}
	if _, err := os.Stat(filepath.Join(dest, "go.mod")); err != nil {
		t.Fatalf("cloned go.mod: %v", err)
	}
}

func TestOpenLocalCopiesFixture(t *testing.T) {
	src := newGitFixture(t)

	opened, err := OpenLocal(src)
	if err != nil {
		t.Fatalf("OpenLocal() error = %v", err)
	}
	if opened.Path == src {
		t.Fatalf("OpenLocal() reused source path")
	}
	if opened.Git == nil {
		t.Fatalf("OpenLocal() Git is nil")
	}

	if err := os.WriteFile(filepath.Join(opened.Path, "extra.txt"), []byte("copy only"), 0o644); err != nil {
		t.Fatalf("write copied file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(src, "extra.txt")); !os.IsNotExist(err) {
		t.Fatalf("source repo was modified, stat err = %v", err)
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		name string
		file string
		want Language
	}{
		{
			name: "go",
			file: "go.mod",
			want: Language{Name: "go", TestCmd: []string{"go", "test", "./..."}, BuildCmd: []string{"go", "build", "./..."}},
		},
		{
			name: "javascript",
			file: "package.json",
			want: Language{Name: "javascript", TestCmd: []string{"npm", "test"}, BuildCmd: []string{"npm", "run", "build"}},
		},
		{
			name: "typescript tsconfig",
			file: "tsconfig.json",
			want: Language{Name: "typescript", TestCmd: []string{"npm", "test"}, BuildCmd: []string{"npm", "run", "build"}},
		},
		{
			name: "python",
			file: "pyproject.toml",
			want: Language{Name: "python", TestCmd: []string{"python", "-m", "pytest"}, BuildCmd: []string{"python", "-m", "compileall", "."}},
		},
		{
			name: "python requirements",
			file: "requirements.txt",
			want: Language{Name: "python", TestCmd: []string{"python", "-m", "pytest"}, BuildCmd: []string{"python", "-m", "compileall", "."}},
		},
		{
			name: "python setup",
			file: "setup.py",
			want: Language{Name: "python", TestCmd: []string{"python", "-m", "pytest"}, BuildCmd: []string{"python", "-m", "compileall", "."}},
		},
		{
			name: "rust",
			file: "Cargo.toml",
			want: Language{Name: "rust", TestCmd: []string{"cargo", "test"}, BuildCmd: []string{"cargo", "build"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			if test.file == "tsconfig.json" {
				if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"test":"echo test","build":"echo build"}}`), 0o644); err != nil {
					t.Fatalf("write package fixture: %v", err)
				}
			}
			if err := os.WriteFile(filepath.Join(dir, test.file), []byte("fixture"), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			got, err := DetectLanguage(dir)
			if err != nil {
				t.Fatalf("DetectLanguage() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("DetectLanguage() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestDetectLanguageTypeScriptPackageDependency(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`{"devDependencies":{"typescript":"^5.0.0"}}`)
	if err := os.WriteFile(filepath.Join(dir, "package.json"), data, 0o644); err != nil {
		t.Fatalf("write package fixture: %v", err)
	}

	got, err := DetectLanguage(dir)
	if err != nil {
		t.Fatalf("DetectLanguage() error = %v", err)
	}
	want := Language{Name: "typescript", TestCmd: []string{"npm", "test"}, BuildCmd: []string{"npm", "run", "build"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DetectLanguage() = %#v, want %#v", got, want)
	}
}

func TestDetectLanguageOverride(t *testing.T) {
	dir := t.TempDir()
	data := []byte(strings.Join([]string{
		"language: ruby",
		"test_cmd: bundle exec rake test",
		"build_cmd:",
		"  - bundle",
		"  - exec",
		"  - rake",
		"  - build",
		"",
	}, "\n"))
	if err := os.WriteFile(filepath.Join(dir, ".codedojo.yaml"), data, 0o644); err != nil {
		t.Fatalf("write override: %v", err)
	}

	got, err := DetectLanguage(dir)
	if err != nil {
		t.Fatalf("DetectLanguage() error = %v", err)
	}
	want := Language{Name: "ruby", TestCmd: []string{"bundle", "exec", "rake", "test"}, BuildCmd: []string{"bundle", "exec", "rake", "build"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DetectLanguage() = %#v, want %#v", got, want)
	}
}

func TestDetectLanguageMalformedOverride(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".codedojo.yaml"), []byte("language: ["), 0o644); err != nil {
		t.Fatalf("write override: %v", err)
	}

	if _, err := DetectLanguage(dir); err == nil {
		t.Fatalf("DetectLanguage() error = nil, want parse error")
	}
}

func TestAuthForURLWithGitHubToken(t *testing.T) {
	auth, err := authForURLWithHints("https://github.com/acme/repo.git", AuthHints{GitHubToken: "secret"})
	if err != nil {
		t.Fatalf("authForURLWithHints() error = %v", err)
	}
	basic, ok := auth.(*githttp.BasicAuth)
	if !ok {
		t.Fatalf("auth type = %T, want *http.BasicAuth", auth)
	}
	if basic.Username != "x-access-token" || basic.Password != "secret" {
		t.Fatalf("auth = %#v", basic)
	}
}

func TestEnvAuthHintsDiscoversSSHKeys(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("create .ssh: %v", err)
	}
	privateKey := filepath.Join(sshDir, "id_ed25519")
	publicKey := privateKey + ".pub"
	if err := os.WriteFile(privateKey, []byte("private"), 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
	if err := os.WriteFile(publicKey, []byte("public"), 0o644); err != nil {
		t.Fatalf("write public key: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GITHUB_TOKEN", "token")
	t.Setenv("GIT_ASKPASS", "/tmp/askpass")

	hints := EnvAuthHints()
	if hints.GitHubToken != "token" || hints.GitAskPass != "/tmp/askpass" {
		t.Fatalf("EnvAuthHints() = %#v", hints)
	}
	if !reflect.DeepEqual(hints.SSHKeys, []string{privateKey}) {
		t.Fatalf("SSHKeys = %#v, want %#v", hints.SSHKeys, []string{privateKey})
	}
}

func newGitFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFixtureFile(t, dir, "go.mod", "module example.com/fixture\n\ngo 1.23\n")
	writeFixtureFile(t, dir, "calculator/calculator.go", "package calculator\n\nfunc Add(a, b int) int { return a + b }\n")
	runGit(t, dir, "init")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "-c", "user.name=CodeDojo", "-c", "user.email=codedojo@example.com", "commit", "-m", "initial")
	return dir
}

func writeFixtureFile(t *testing.T, root, rel, data string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture path: %v", err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
}
