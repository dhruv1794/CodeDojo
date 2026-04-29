# Security

CodeDojo runs commands from repositories that may be unfamiliar or intentionally broken. Treat every practice repository as untrusted code.

The MVP includes two sandbox drivers:

- Docker driver: preferred when Docker is reachable
- Local driver: development fallback only, not a security boundary

## Threat Model

CodeDojo tries to reduce risk from:

- test commands that execute arbitrary code
- build scripts or package hooks inside practice repositories
- malicious or accidental filesystem writes during a session
- runaway CPU, memory, process, or time usage
- network access from sandboxed commands
- coach responses that reveal implementation details

CodeDojo does not currently defend against:

- Docker daemon compromise
- kernel or container runtime vulnerabilities
- malicious code run through the local driver
- malicious repositories exploiting the host Git client during clone/open
- denial of service against the host outside configured Docker limits
- secrets already present in the repository copy

## Docker Sandbox

When Docker is reachable, CLI flows use `internal/sandbox/docker`.

The Docker driver:

- copies the selected repository into a temporary workspace
- starts a long-running container from `codedojo/go:1.23` by default
- uses `/workspace` as the working directory
- mounts only the temporary workspace read-write
- sets the container root filesystem read-only
- drops all Linux capabilities
- sets `no-new-privileges`
- disables networking for the default `NetworkNone` policy
- applies memory, CPU, and PID limits from `sandbox.Spec`
- sets `HOME`, `TMPDIR`, `GOCACHE`, and `GOMODCACHE` inside `/workspace`
- removes the container and temporary workspace on close

This is the expected mode for running untrusted practice repositories.

### Docker Limitations

Docker isolation is still shared-kernel isolation. It depends on:

- a correctly configured Docker daemon
- host kernel and container runtime security
- the configured image not containing unexpected privileged tooling
- the user not mounting sensitive host paths into the workspace

Do not run CodeDojo against hostile repositories on a machine that holds sensitive credentials unless you understand and accept the Docker risk.

## Local Sandbox

When Docker is unavailable, CodeDojo falls back to `internal/sandbox/local` and prints a warning.

The local driver:

- copies the repository into a temporary directory
- runs commands directly on the host with `exec.CommandContext`
- does not enforce network policy
- does not enforce CPU limits
- does not enforce memory limits
- does not isolate processes beyond normal OS user permissions
- removes the temporary directory on close

The local driver is for development, tests, and trusted repositories only. It does not prevent arbitrary code from reading environment variables, accessing the network, writing outside the workspace, spawning child processes, or using host credentials available to the current user.

## Filesystem Handling

CodeDojo avoids modifying the original local repository path. `repo.OpenLocal` copies the source repository into a temporary directory before task generation. Sandbox drivers then copy that working tree again into their own temporary workspace.

Sandbox `ReadFile` and `WriteFile` methods reject non-local paths using `filepath.IsLocal`, so direct REPL file operations cannot use absolute paths or `..` escapes. This guard applies to CodeDojo file helper methods, not to arbitrary commands run inside the sandbox.

Important limitation: repository copy currently preserves symlinks. A malicious repository can contain symlinks that point outside the workspace. The Docker driver limits the impact because only the temporary workspace is mounted, but the local driver runs on the host and should not be used for untrusted symlink-heavy repositories.

## Network Policy

The default CLI sandbox spec uses `NetworkNone`.

In Docker mode, this maps to Docker networking disabled / `none`.

In local mode, network policy is logged but not enforced. Commands run by the local driver can use the host network.

`NetworkRestricted` currently maps to `none` in Docker mode. There is no allowlist-based restricted network implementation yet.

## Resource Limits

The Docker driver applies:

- memory limit
- memory swap equal to memory limit
- CPU quota through NanoCPUs
- PID limit
- command timeout

The local driver only respects command context cancellation. It does not enforce memory, CPU, or PID limits.

## Secrets

Avoid placing secrets in practice repositories. CodeDojo copies the repository contents into temporary workspaces, and commands may read files available inside those copies.

Coach backend secrets:

- Anthropic uses the config API key or `ANTHROPIC_API_KEY`
- Ollama can use `OLLAMA_MODEL` and `OLLAMA_BASE_URL`

Docker sandbox commands do not receive arbitrary host environment variables by default; the driver sets a small workspace-specific environment. Local sandbox commands inherit the process environment through Go's default command behavior when no explicit environment is set, so local mode can expose host environment variables to untrusted commands.

## Coach Output Safety

Coach hints are wrapped by `coach.RetryWithStricterPrompt`.

The validator rejects:

- fenced code blocks with more than three non-empty lines
- function or method definitions in supported languages
- task-specific banned identifiers

This protects the learning loop from accidental solution leaks. It is not a security control for executing untrusted code.

## Recommended Usage

For trusted local development:

```sh
./bin/codedojo review --repo testdata/sample-go-repo
```

For unfamiliar repositories:

```sh
make images
./bin/codedojo review --repo <repo-url-or-copy>
```

Confirm Docker is available before starting. If CodeDojo prints a local fallback warning, stop and fix Docker before using an untrusted repository.

## Reporting Security Issues

Until a formal security policy is added, do not file public issues for exploitable vulnerabilities. Contact the repository maintainer privately, include reproduction steps, and describe whether the issue affects Docker mode, local mode, coach output, or repository handling.

## Hardening Backlog

Known follow-up work:

- document a formal private vulnerability reporting address before public launch
- make local mode opt-in for non-test CLI use
- add symlink policy checks during repository copy
- add image provenance and digest pinning for sandbox images
- add optional Docker seccomp/AppArmor profiles
- add allowlist-based restricted networking if `NetworkRestricted` is exposed to users
- scrub or explicitly set the environment for local sandbox commands
