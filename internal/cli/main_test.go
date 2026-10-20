package cli

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestMain makes the package hermetic on git identity: several tests drive
// commands that shell out to `git commit` (source new, harvest ratify), and
// the product deliberately uses the OPERATOR's identity - runners have none,
// and a test run must not depend on the developer's global config either.
// When no identity is configured anywhere, a synthetic one is injected via
// the GIT_AUTHOR/COMMITTER env (subprocesses inherit it; env wins over any
// config, which is exactly what a runner without config needs).
func TestMain(m *testing.M) {
	if os.Getenv("GIT_AUTHOR_NAME") == "" {
		out, err := exec.Command("git", "config", "--get", "user.email").Output()
		if err != nil || strings.TrimSpace(string(out)) == "" {
			os.Setenv("GIT_AUTHOR_NAME", "harnessator-test")
			os.Setenv("GIT_AUTHOR_EMAIL", "test@harnessator.local")
			os.Setenv("GIT_COMMITTER_NAME", "harnessator-test")
			os.Setenv("GIT_COMMITTER_EMAIL", "test@harnessator.local")
		}
	}
	os.Exit(m.Run())
}
