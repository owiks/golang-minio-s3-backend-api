package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMigrationMakeTargetsRequireAnExplicitOfflineAssertion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		target  string
		offline string
		active  string
		psError string
		want    []string
		wantErr bool
	}{
		{name: "fresh-up", target: "migrate-up", want: []string{"run ./src migrate up"}},
		{name: "explicit-offline-up", target: "migrate-up", offline: "1", want: []string{"run ./src migrate up --offline"}},
		{name: "down-needs-confirmation", target: "migrate-down", wantErr: true},
		{name: "down-rejects-other-values", target: "migrate-down", offline: "yes", wantErr: true},
		{name: "explicit-offline-down", target: "migrate-down", offline: "1", want: []string{"run ./src migrate down --offline"}},
		{name: "docker-up-checks-before-build-or-stop", target: "docker-up", wantErr: true},
		{name: "docker-migrate-needs-confirmation", target: "docker-migrate", wantErr: true},
		{
			name: "docker-migrate-refuses-active-api", target: "docker-migrate", offline: "1", active: "running-api-id", wantErr: true,
			want: []string{"-f docker-compose.yml ps --all --status running --status restarting --status paused --quiet api"},
		},
		{
			name: "docker-migrate-refuses-unknown-status", target: "docker-migrate", offline: "1", psError: "1", wantErr: true,
			want: []string{"-f docker-compose.yml ps --all --status running --status restarting --status paused --quiet api"},
		},
		{
			name: "docker-up-verifies-stop", target: "docker-up", offline: "1", active: "still-active-api-id", wantErr: true,
			want: []string{
				"-f docker-compose.yml build",
				"-f docker-compose.yml stop api",
				"-f docker-compose.yml ps --all --status running --status restarting --status paused --quiet api",
			},
		},
		{
			name: "docker-up-sequence", target: "docker-up", offline: "1",
			want: []string{
				"-f docker-compose.yml build",
				"-f docker-compose.yml stop api",
				"-f docker-compose.yml ps --all --status running --status restarting --status paused --quiet api",
				"-f docker-compose.yml run --rm --no-deps state-tools migrate up --offline",
				"-f docker-compose.yml up -d --no-build --wait",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if runtime.GOOS == "windows" {
				t.Skip("Make helpers require a POSIX shell")
			}
			makePath, err := exec.LookPath("make")
			if err != nil {
				t.Skip("make is unavailable")
			}
			root, err := filepath.Abs("../..")
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			log := filepath.Join(dir, "commands.log")
			fake := filepath.Join(dir, "tool")
			const script = `#!/bin/sh
printf '%s\n' "$*" >> "$MIGRATION_TEST_LOG"
case "$*" in
  *" ps "*)
    test "$MIGRATION_TEST_PS_ERROR" != 1 || exit 1
    test -z "$MIGRATION_TEST_ACTIVE" || printf '%s\n' "$MIGRATION_TEST_ACTIVE"
    ;;
esac
exit 0
`
			if err = os.WriteFile(fake, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("MIGRATION_TEST_LOG", log)
			t.Setenv("MIGRATION_TEST_ACTIVE", tc.active)
			t.Setenv("MIGRATION_TEST_PS_ERROR", tc.psError)
			t.Setenv("MAKEFLAGS", "")
			t.Setenv("MFLAGS", "")
			cmd := exec.Command(makePath, "--no-print-directory", tc.target, "GO="+fake, "COMPOSE="+fake, "OFFLINE="+tc.offline)
			cmd.Dir = root
			output, err := cmd.CombinedOutput()
			if (err != nil) != tc.wantErr {
				t.Fatalf("make error = %v, want error %v: %s", err, tc.wantErr, output)
			}
			raw, err := os.ReadFile(log)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if got, want := strings.TrimSpace(string(raw)), strings.Join(tc.want, "\n"); got != want {
				t.Fatalf("commands = %q, want %q", got, want)
			}
		})
	}
}
