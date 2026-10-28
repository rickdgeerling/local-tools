package main

import "testing"

func TestIsLockfilePath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"package-lock.json", true},
		{"sub/dir/package-lock.json", true},
		{"yarn.lock", true},
		{"Cargo.lock", true}, // case-insensitive
		{"sub/dir/go.sum", true},
		{"pnpm-lock.yaml", true},
		{"main.go", false},
		{"lockfile.go", false},
		{"src/yarn.lock.bak", false}, // suffix only, not an exact basename
		{"", false},
	}
	for _, c := range cases {
		if got := isLockfilePath(c.path); got != c.want {
			t.Errorf("isLockfilePath(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestParseDiffGitPaths(t *testing.T) {
	cases := []struct {
		line  string
		wantA string
		wantB string
	}{
		{"diff --git a/main.go b/main.go", "main.go", "main.go"},
		{"diff --git a/old.lock b/new.lock", "old.lock", "new.lock"},
		{"diff --git a/deep/dir/x.txt b/deep/dir/x.txt", "deep/dir/x.txt", "deep/dir/x.txt"},
		{`diff --git "a/spa ced" "b/spa ced"`, "spa ced", "spa ced"},
		{"diff --git a/spa ced b/spa ced", "", ""}, // ambiguous, fail-open
		{"not a diff header", "", ""},
	}
	for _, c := range cases {
		gotA, gotB := parseDiffGitPaths(c.line)
		if gotA != c.wantA || gotB != c.wantB {
			t.Errorf("parseDiffGitPaths(%q) = (%q, %q), want (%q, %q)", c.line, gotA, gotB, c.wantA, c.wantB)
		}
	}
}

func TestStripLockfileDiffs(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "lockfile section replaced, source file kept",
			in: "diff --git a/main.go b/main.go\n" +
				"index 111..222 100644\n" +
				"--- a/main.go\n" +
				"+++ b/main.go\n" +
				"@@ -1,3 +1,3 @@\n" +
				"-old\n" +
				"+new\n" +
				"diff --git a/package-lock.json b/package-lock.json\n" +
				"index 333..444 100644\n" +
				"--- a/package-lock.json\n" +
				"+++ b/package-lock.json\n" +
				"@@ -1,5000 +1,5000 @@\n" +
				"-\"lodash\": \"4.17.20\"\n" +
				"+\"lodash\": \"4.17.21\"\n",
			want: "diff --git a/main.go b/main.go\n" +
				"index 111..222 100644\n" +
				"--- a/main.go\n" +
				"+++ b/main.go\n" +
				"@@ -1,3 +1,3 @@\n" +
				"-old\n" +
				"+new\n" +
				"diff --git a/package-lock.json b/package-lock.json\n" +
				"[SKIPPED LOCKFILE] (6 lines omitted)\n",
		},
		{
			name: "lockfile at end of diff",
			in: "diff --git a/yarn.lock b/yarn.lock\n" +
				"@@ -1,9 +1,9 @@\n" +
				" some line\n",
			want: "diff --git a/yarn.lock b/yarn.lock\n" +
				"[SKIPPED LOCKFILE] (2 lines omitted)\n",
		},
		{
			name: "lockfile in subdirectory and mixed case",
			in: "diff --git a/apps/web/Cargo.lock b/apps/web/Cargo.lock\n" +
				"@@ -1,2 +1,2 @@\n" +
				" x\n",
			want: "diff --git a/apps/web/Cargo.lock b/apps/web/Cargo.lock\n" +
				"[SKIPPED LOCKFILE] (2 lines omitted)\n",
		},
		{
			name: "renamed lockfile",
			in: "diff --git a/old.sum b/go.sum\n" +
				"similarity index 90%\n" +
				"rename from old.sum\n" +
				"rename to go.sum\n",
			want: "diff --git a/old.sum b/go.sum\n" +
				"[SKIPPED LOCKFILE] (3 lines omitted)\n",
		},
		{
			name: "unparseable header is passed through (fail-open)",
			in: "diff --git a/spa ced b/spa ced\n" +
				"@@ -1,2 +1,2 @@\n" +
				" x\n",
			want: "diff --git a/spa ced b/spa ced\n" +
				"@@ -1,2 +1,2 @@\n" +
				" x\n",
		},
		{
			name: "no lockfiles: exact round trip",
			in: "diff --git a/main.go b/main.go\n" +
				"@@ -1,2 +1,2 @@\n" +
				"-a\n" +
				"+b\n",
			want: "diff --git a/main.go b/main.go\n" +
				"@@ -1,2 +1,2 @@\n" +
				"-a\n" +
				"+b\n",
		},
		{
			name: "no trailing newline preserved",
			in:   "diff --git a/main.go b/main.go\n+hello",
			want: "diff --git a/main.go b/main.go\n+hello",
		},
		{
			name: "empty diff",
			in:   "",
			want: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := stripLockfileDiffs(c.in); got != c.want {
				t.Errorf("stripLockfileDiffs() mismatch:\ngot:\n%q\nwant:\n%q", got, c.want)
			}
		})
	}
}
