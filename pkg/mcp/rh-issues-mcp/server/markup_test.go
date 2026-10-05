package server

import (
	"strings"
	"testing"
)

func TestJiraToMarkdownHeadingsListsFences(t *testing.T) {
	// Structured like real Red Hat Jira descriptions.
	in := "h2. Impact\n\nWhen ~70 primary UDNs are created, ARP fails.\n\n* one\n* two\n* nested items:\n** inner\n\n# first\n# second\n\n{noformat}# 1. Create UDNs\nfor i in $(seq 0 69); do\n  oc get nodes -o jsonpath='{.x}'\ndone{noformat}\n\nbq. quoted line\n"
	want := "## Impact\n\nWhen ~70 primary UDNs are created, ARP fails.\n\n- one\n- two\n- nested items:\n  - inner\n\n1. first\n2. second\n\n```\n# 1. Create UDNs\nfor i in $(seq 0 69); do\n  oc get nodes -o jsonpath='{.x}'\ndone\n```\n\n> quoted line\n"

	if got := jiraToMarkdown(in); got != want {
		t.Errorf("jiraToMarkdown mismatch:\ngot:\n%q\nwant:\n%q", got, want)
	}
}

func TestJiraToMarkdownInline(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"heading levels", "h1. A\nh3. B\nh6. C", "# A\n### B\n###### C"},
		{"mono", "run {{oc get nodes}} now", "run `oc get nodes` now"},
		{"named link", "see [docs|https://example.com/a]", "see [docs](https://example.com/a)"},
		{"smart link", "[slides|https://x.io/s|smart-link]", "[slides](https://x.io/s)"},
		{"bare link", "go to [https://example.com]", "go to [https://example.com](https://example.com)"},
		{"mention", "reported by [~accountid:712020:abc]", "reported by @user"},
		{"bold", "this is *very important* text", "this is **very important** text"},
		{"bold at bounds", "*start* and *end*", "**start** and **end**"},
		{"no bold across words", "2*3*4 stays", "2*3*4 stays"},
		{"color strip", "{color:red}alert{color} done", "alert done"},
		{"panel title", "{panel:title=Diagnosis}\nbody text\n{panel}", "**Diagnosis**\n\nbody text"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := jiraToMarkdown(tt.in); got != tt.want {
				t.Errorf("jiraToMarkdown(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestJiraToMarkdownTables(t *testing.T) {
	in := "||Cluster||Version||\n|4.12|ok|\n|4.13|bad|"
	want := "| Cluster | Version |\n|---|---|\n|4.12|ok|\n|4.13|bad|"
	if got := jiraToMarkdown(in); got != want {
		t.Errorf("table mismatch:\ngot:\n%q\nwant:\n%q", got, want)
	}
}

func TestJiraToMarkdownCodeFenceWithLang(t *testing.T) {
	in := "{code:bash}\noc get nodes --show-labels\n* not a bullet\n{code}"
	want := "```bash\noc get nodes --show-labels\n* not a bullet\n```"
	if got := jiraToMarkdown(in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestJiraToMarkdownUnterminatedFence(t *testing.T) {
	in := "{noformat}\ninside"
	got := jiraToMarkdown(in)
	if !strings.HasPrefix(got, "```\ninside") || !strings.HasSuffix(got, "```") {
		t.Errorf("unterminated fence should close itself, got %q", got)
	}
}

func TestJiraToMarkdownEscapedFences(t *testing.T) {
	// Migration artifacts sometimes backslash-escape the markers.
	in := "\\{code:java}\nif !deleting {\n    return nil\n}\n\\{code}\nafter"
	want := "```java\nif !deleting {\n    return nil\n}\n```\nafter"
	if got := jiraToMarkdown(in); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestJiraToMarkdownEmpty(t *testing.T) {
	if got := jiraToMarkdown(""); got != "" {
		t.Errorf("empty input should stay empty, got %q", got)
	}
}
