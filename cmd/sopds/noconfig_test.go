package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCommandsThatNeedNoConfig guards the informational commands: `version` and
// `help` must work where no config is readable — which is exactly the situation
// on the server (`sudo -u sopds /opt/sopds/sopds version` from a foreign cwd).
// It builds the binary and runs it in an empty dir, because the failure lived in
// cobra's hook wiring and only shows up through a real invocation.
func TestCommandsThatNeedNoConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "sopds")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	// Run from an empty dir with a bare env, so no config.yaml is discoverable
	// next to the cwd or under $HOME.
	run := func(args ...string) (string, error) {
		cmd := exec.Command(bin, args...)
		cmd.Dir = t.TempDir()
		cmd.Env = append(os.Environ(), "HOME="+dir)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"version"}, "Rev " + revision},
		{[]string{"help"}, "OPDS catalog server"},
		{[]string{"help", "version"}, "Print version information"},
		{[]string{"--help"}, "OPDS catalog server"},
	} {
		name := strings.Join(tc.args, " ")
		out, err := run(tc.args...)
		if err != nil {
			t.Errorf("%q failed without a config: %v\n%s", name, err, out)
			continue
		}
		if !strings.Contains(out, tc.want) {
			t.Errorf("%q output missing %q:\n%s", name, tc.want, out)
		}
	}

	// A command that genuinely needs the config must still say so, otherwise the
	// fix above has been over-applied.
	if out, err := run("status"); err == nil {
		t.Errorf("`status` should fail without a config, got:\n%s", out)
	} else if !strings.Contains(out, "failed to load config") {
		t.Errorf("`status` failed for the wrong reason:\n%s", out)
	}
}
