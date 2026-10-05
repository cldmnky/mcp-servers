package server

import (
	"regexp"
	"strings"
)

// Jira issues store their description in Jira wiki markup (h2. headings,
// {noformat} blocks, *bold*, [title|url] links, ...). MCP clients and their
// models read Markdown far better, so convert the common constructs and pass
// everything else through unchanged.

var (
	// Jira sometimes stores fences backslash-escaped (\{code}, from
	// migrations or literal escaping), so both forms are matched.
	jiraFenceOpen  = regexp.MustCompile(`^\\?\{(noformat|code)(?::([^}]*))?\}`)
	jiraFenceClose = regexp.MustCompile(`^(.*?)\\?\{(noformat|code)\}(.*)$`)
	jiraHeading    = regexp.MustCompile(`^h([1-6])\.\s*(.*)$`)
	jiraBullet     = regexp.MustCompile(`^(\s*)(\*+|-)\s+`)
	jiraOrdered    = regexp.MustCompile(`^(\s*)#\s+`)
	jiraQuoteLine  = regexp.MustCompile(`^bq\.\s*(.*)$`)
	jiraPanelTitle = regexp.MustCompile(`^\{panel(?::title=([^}]*))?\}\s*$`)
	jiraTableHead  = regexp.MustCompile(`^\s*\|\|`)
	jiraMono       = regexp.MustCompile(`\{\{([^{}]+)\}\}`)
	jiraLink       = regexp.MustCompile(`\[([^\]|]+)\|([^\]]+)\]`)
	jiraBareLink   = regexp.MustCompile(`\[((?:https?://|mailto:)[^\]]+)\]`)
	jiraMention    = regexp.MustCompile(`\[~[^\]]+\]`)
	jiraBold       = regexp.MustCompile(`(^|\W)\*([^*\s][^*\n]*?)\*($|\W|[,.;:!?)])`)
	jiraColor      = regexp.MustCompile(`\{color[^}]*\}`)
)

// jiraToMarkdown converts Jira wiki markup to Markdown. Unknown constructs
// are left as-is rather than guessed at, so content is never lost.
func jiraToMarkdown(s string) string {
	if s == "" {
		return s
	}

	lines := strings.Split(s, "\n")
	var out []string
	inFence := false
	ordered := 0
	prevOrdered := false
	prevWasTable := false

	for _, line := range lines {
		// Fenced blocks pass through untouched. The closing marker may be
		// glued to the last content line, so split on first occurrence.
		if inFence {
			if m := jiraFenceClose.FindStringSubmatch(line); m != nil {
				if strings.TrimSpace(m[1]) != "" {
					out = append(out, m[1])
				}
				out = append(out, "```")
				inFence = false
				if rest := strings.TrimSpace(m[3]); rest != "" {
					out = append(out, jiraInline(rest))
				}
			} else {
				out = append(out, line)
			}
			continue
		}

		trimmed := strings.TrimSpace(line)
		if m := jiraFenceOpen.FindStringSubmatch(trimmed); m != nil {
			inFence = true
			lang := strings.TrimSpace(m[2])
			out = append(out, "```"+lang)
			rest := trimmed[len(m[0]):]
			if close := jiraFenceClose.FindStringSubmatch(rest); close != nil {
				// A block may open and close on the same line. Keep its code
				// verbatim and resume prose conversion after the closing marker.
				if strings.TrimSpace(close[1]) != "" {
					out = append(out, close[1])
				}
				out = append(out, "```")
				inFence = false
				if tail := strings.TrimSpace(close[3]); tail != "" {
					out = append(out, jiraInline(tail))
				}
			} else if rest = strings.TrimSpace(rest); rest != "" {
				out = append(out, rest)
			}
			continue
		}
		if m := jiraHeading.FindStringSubmatch(trimmed); m != nil {
			out = append(out, strings.Repeat("#", atoi(m[1]))+" "+jiraInline(m[2]))
			prevWasTable = false
			continue
		}
		if m := jiraQuoteLine.FindStringSubmatch(trimmed); m != nil {
			out = append(out, "> "+jiraInline(m[1]))
			prevWasTable = false
			continue
		}
		if m := jiraPanelTitle.FindStringSubmatch(trimmed); m != nil {
			if m[1] != "" {
				out = append(out, "**"+m[1]+"**", "")
			}
			prevWasTable = false
			continue
		}
		if trimmed == "{quote}" {
			prevWasTable = false
			continue
		}
		if jiraTableHead.MatchString(line) {
			row := jiraTableRow(line)
			out = append(out, row)
			if !prevWasTable {
				out = append(out, jiraTableSeparator(row))
			}
			prevWasTable = true
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "|") {
			out = append(out, line)
			prevWasTable = true
			continue
		}
		prevWasTable = false

		if m := jiraBullet.FindStringSubmatch(line); m != nil {
			// Jira nests with repeated markers (**, ***); indent 2 spaces per
			// level, plus any leading whitespace the author already had.
			level := len(m[2])
			indent := strings.Repeat("  ", level-1) + m[1]
			rest := strings.TrimPrefix(line[len(m[1]):], m[2])
			rest = strings.TrimLeft(rest, " ")
			out = append(out, indent+"- "+jiraInline(rest))
			continue
		}
		if m := jiraOrdered.FindStringSubmatch(line); m != nil {
			ordered++
			prevOrdered = true
			out = append(out, m[1]+itoa(ordered)+". "+jiraInline(line[len(m[0]):]))
			continue
		}
		if prevOrdered {
			ordered = 0
			prevOrdered = false
		}
		out = append(out, jiraInline(line))
	}
	if inFence { // unterminated fence in the source
		out = append(out, "```")
	}
	return strings.Join(out, "\n")
}

// jiraInline converts inline wiki markup outside of code blocks.
func jiraInline(s string) string {
	s = jiraColor.ReplaceAllString(s, "")
	s = jiraMono.ReplaceAllString(s, "`$1`")
	// [title|url] and [title|url|smart-link] -> [title](url)
	s = jiraLink.ReplaceAllStringFunc(s, func(r string) string {
		m := jiraLink.FindStringSubmatch(r)
		url := m[2]
		if i := strings.LastIndex(url, "|"); i >= 0 {
			url = url[:i]
		}
		return "[" + m[1] + "](" + url + ")"
	})
	s = jiraBareLink.ReplaceAllString(s, "[$1]($1)")
	s = jiraMention.ReplaceAllString(s, "@user")
	s = jiraBold.ReplaceAllString(s, "$1**$2**$3")
	return s
}

// jiraTableRow converts a ||a||b|| header row to | a | b |.
func jiraTableRow(line string) string {
	cells := strings.Split(strings.TrimSpace(line), "||")
	var parts []string
	for _, c := range cells {
		if strings.TrimSpace(c) != "" {
			parts = append(parts, " "+strings.TrimSpace(c)+" ")
		}
	}
	return "|" + strings.Join(parts, "|") + "|"
}

func jiraTableSeparator(headerRow string) string {
	n := strings.Count(headerRow, "|") - 1
	if n < 1 {
		return "|---|"
	}
	return "|" + strings.TrimSuffix(strings.Repeat("---|", n), "|") + "|"
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
