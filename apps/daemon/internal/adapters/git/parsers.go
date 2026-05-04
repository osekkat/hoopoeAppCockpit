// parsers.go — output parsers for the wrapped git commands.
//
// Each parser is total (returns sensible empty results on empty input)
// and tolerates trailing whitespace, missing optional fields, and
// version differences in git's output. Failure modes that indicate a
// malformed invocation surface as errors; benign variations don't.
package git

import (
	"bufio"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseStatus parses `git status --porcelain=v1 --branch` output.
//
// The first line is the branch header: "## main...origin/main [ahead 1, behind 2]"
// or "## HEAD (no branch)" for detached state. Subsequent lines are
// XY-prefixed entries.
func parseStatus(out []byte) (*Status, error) {
	s := &Status{AheadBy: -1, BehindBy: -1}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			first = false
			if strings.HasPrefix(line, "## ") {
				parseStatusBranchHeader(s, line[3:])
				continue
			}
			// Some git versions skip the header on detached HEAD; treat
			// the line as an entry instead.
		}
		if line == "" {
			continue
		}
		if len(line) < 3 {
			continue
		}
		entry := StatusEntry{XY: line[:2], Path: line[3:]}
		// Rename / copy entries are formatted as "XY old -> new".
		if entry.XY[0] == 'R' || entry.XY[0] == 'C' {
			parts := strings.SplitN(entry.Path, " -> ", 2)
			if len(parts) == 2 {
				entry.OldPath = parts[0]
				entry.Path = parts[1]
			}
		}
		s.Entries = append(s.Entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("git: scan status output: %w", err)
	}
	s.Clean = len(s.Entries) == 0 && (s.AheadBy <= 0)
	return s, nil
}

func parseStatusBranchHeader(s *Status, payload string) {
	// Examples:
	//   "main...origin/main"
	//   "main...origin/main [ahead 1]"
	//   "main...origin/main [ahead 1, behind 2]"
	//   "HEAD (no branch)"
	//   "main"  (no upstream)
	if strings.HasPrefix(payload, "HEAD (no branch)") {
		s.Branch = "HEAD"
		s.Detached = true
		return
	}
	// Bracket suffix is optional; split it off first.
	if idx := strings.Index(payload, " ["); idx > 0 {
		bracket := strings.TrimSpace(payload[idx+2:])
		bracket = strings.TrimSuffix(bracket, "]")
		for _, part := range strings.Split(bracket, ", ") {
			switch {
			case strings.HasPrefix(part, "ahead "):
				if n, err := strconv.Atoi(strings.TrimPrefix(part, "ahead ")); err == nil {
					s.AheadBy = n
				}
			case strings.HasPrefix(part, "behind "):
				if n, err := strconv.Atoi(strings.TrimPrefix(part, "behind ")); err == nil {
					s.BehindBy = n
				}
			case part == "gone":
				s.Upstream = ""
			}
		}
		payload = payload[:idx]
	}
	if idx := strings.Index(payload, "..."); idx > 0 {
		s.Branch = payload[:idx]
		s.Upstream = payload[idx+3:]
		// Default ahead/behind to 0 when upstream is set but no bracket present.
		if s.AheadBy < 0 {
			s.AheadBy = 0
		}
		if s.BehindBy < 0 {
			s.BehindBy = 0
		}
	} else {
		s.Branch = strings.TrimSpace(payload)
	}
}

// parseLog parses the NUL-separated --pretty=format output the Log()
// method requests. Records are separated by 0x1e (ASCII RS).
func parseLog(out []byte) ([]Commit, error) {
	if len(out) == 0 {
		return nil, nil
	}
	rawRecords := strings.Split(string(out), "\x1e")
	commits := make([]Commit, 0, len(rawRecords))
	for _, rec := range rawRecords {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		fields := strings.Split(rec, "\x00")
		if len(fields) < 11 {
			return nil, fmt.Errorf("git: malformed log record (%d fields, want >=11)", len(fields))
		}
		c := Commit{
			SHA:            fields[0],
			ShortSHA:       fields[1],
			AuthorName:     fields[2],
			AuthorEmail:    fields[3],
			CommitterName:  fields[5],
			CommitterEmail: fields[6],
			Subject:        fields[9],
			Body:           strings.TrimRight(fields[10], "\n"),
		}
		if t, err := time.Parse(time.RFC3339, fields[4]); err == nil {
			c.AuthoredAt = t
		}
		if t, err := time.Parse(time.RFC3339, fields[7]); err == nil {
			c.CommittedAt = t
		}
		if fields[8] != "" {
			c.ParentSHAs = strings.Split(fields[8], " ")
		}
		commits = append(commits, c)
	}
	return commits, nil
}

// parsePushPorcelain parses `git push --porcelain` output. Lines:
//
//   "To <url>"
//   "<flag><tab><src>:<dst><tab><summary>"  (per ref)
//   "Done"
//
// flag: ' ' (ff), '+' (forced), '*' (new), '!' (rejected), '-' (deleted),
// '=' (up to date).
func parsePushPorcelain(out []byte, forced bool) *PushResult {
	result := &PushResult{OK: true, Forced: forced}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "To "):
			result.RemoteRef = strings.TrimPrefix(line, "To ")
		case line == "Done":
			// terminator; no-op
		case len(line) >= 1 && (line[0] == ' ' || line[0] == '+' || line[0] == '*' || line[0] == '!' || line[0] == '-' || line[0] == '='):
			parts := strings.Split(line, "\t")
			if len(parts) < 3 {
				continue
			}
			refParts := strings.SplitN(parts[1], ":", 2)
			update := PushedRefUpdate{
				Status:      string(line[0]),
				Source:      refParts[0],
				Destination: parts[1],
				Summary:     parts[2],
			}
			if len(refParts) == 2 {
				update.Destination = refParts[1]
			}
			if update.Status == "!" {
				result.OK = false
				update.Reason = parts[2]
			}
			result.UpdatedRefs = append(result.UpdatedRefs, update)
		}
	}
	if len(result.UpdatedRefs) > 0 {
		first := result.UpdatedRefs[0]
		result.Summary = first.Summary
		// "<old>..<new>" parse — best-effort.
		if idx := strings.Index(first.Summary, ".."); idx > 0 {
			result.OldSHA = first.Summary[:idx]
			result.NewSHA = first.Summary[idx+2:]
		}
	}
	return result
}

// parseLsTree parses `git ls-tree -r <ref>`:
//
//   <mode> <type> <sha>\t<path>
func parseLsTree(out []byte) ([]LsTreeEntry, error) {
	var entries []LsTreeEntry
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		// Format: "<mode> <type> <sha>\t<path>"
		tabIdx := strings.IndexByte(line, '\t')
		if tabIdx < 0 {
			continue
		}
		left := line[:tabIdx]
		path := line[tabIdx+1:]
		fields := strings.Fields(left)
		if len(fields) != 3 {
			continue
		}
		entries = append(entries, LsTreeEntry{
			Mode: fields[0],
			Type: fields[1],
			SHA:  fields[2],
			Path: path,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("git: scan ls-tree output: %w", err)
	}
	return entries, nil
}

// parseBlamePorcelain parses `git blame --porcelain` output.
//
// Format (per line of source):
//   <sha> <orig-line> <final-line> [<group-size>]
//   author <name>
//   author-mail <email>
//   author-time <unix-ts>
//   author-tz <tz>
//   committer ...
//   summary <subject>
//   ...
//   \t<source-line-content>
//
// We capture sha + author-name + author-mail + author-time per line and
// the final tabbed content.
func parseBlamePorcelain(out []byte, path string) (*FileBlame, error) {
	blame := &FileBlame{Path: path}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	type pending struct {
		sha   string
		name  string
		email string
		ts    int64
		line  int
	}
	var current pending
	have := false

	for scanner.Scan() {
		raw := scanner.Text()
		switch {
		case strings.HasPrefix(raw, "\t"):
			// Source line content; emit pending entry.
			if have {
				blame.Lines = append(blame.Lines, BlameLine{
					SHA:         current.sha,
					AuthorName:  current.name,
					AuthorEmail: strings.Trim(current.email, "<>"),
					AuthoredAt:  time.Unix(current.ts, 0).UTC(),
					LineNum:     current.line,
					Content:     raw[1:],
				})
			}
			current = pending{}
			have = false
		case strings.HasPrefix(raw, "author "):
			current.name = strings.TrimPrefix(raw, "author ")
		case strings.HasPrefix(raw, "author-mail "):
			current.email = strings.TrimPrefix(raw, "author-mail ")
		case strings.HasPrefix(raw, "author-time "):
			if ts, err := strconv.ParseInt(strings.TrimPrefix(raw, "author-time "), 10, 64); err == nil {
				current.ts = ts
			}
		default:
			// First line of each group: "<sha> <orig> <final> [<group>]"
			fields := strings.Fields(raw)
			if len(fields) >= 3 {
				if len(fields[0]) == 40 {
					current.sha = fields[0]
					if n, err := strconv.Atoi(fields[2]); err == nil {
						current.line = n
					}
					have = true
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("git: scan blame output: %w", err)
	}
	if len(blame.Lines) == 0 && have {
		// Reached EOF before content line — surface as parse error.
		return nil, errors.New("git: blame output missing tab-prefixed content lines")
	}
	return blame, nil
}

// parseRemoteV parses `git remote -v` output:
//
//   <name>\t<url> (fetch)
//   <name>\t<url> (push)
func parseRemoteV(out []byte) []Remote {
	byName := map[string]*Remote{}
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		tabIdx := strings.IndexByte(line, '\t')
		if tabIdx < 0 {
			continue
		}
		name := line[:tabIdx]
		rest := line[tabIdx+1:]
		urlEnd := strings.LastIndex(rest, " ")
		if urlEnd < 0 {
			continue
		}
		url := rest[:urlEnd]
		role := strings.TrimSpace(rest[urlEnd+1:])
		role = strings.TrimPrefix(role, "(")
		role = strings.TrimSuffix(role, ")")

		r, ok := byName[name]
		if !ok {
			r = &Remote{Name: name}
			byName[name] = r
		}
		switch role {
		case "fetch":
			r.FetchURL = url
		case "push":
			r.PushURL = url
		}
	}
	out2 := make([]Remote, 0, len(byName))
	for _, r := range byName {
		out2 = append(out2, *r)
	}
	return out2
}

// parseBranchV parses `git branch -v --no-abbrev`:
//
//   "* main      <sha> <subject>"
//   "  topic     <sha> <subject>"
//   "  (HEAD detached at <sha>) <sha> <subject>"
func parseBranchV(out []byte) []Branch {
	var branches []Branch
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		isCurrent := strings.HasPrefix(line, "* ")
		if isCurrent || strings.HasPrefix(line, "  ") {
			line = line[2:]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// Detached header: "(HEAD detached at <sha>)"
		if strings.HasPrefix(fields[0], "(HEAD") {
			continue
		}
		b := Branch{
			Name:      fields[0],
			HeadSHA:   fields[1],
			IsCurrent: isCurrent,
		}
		if len(fields) > 2 {
			b.Subject = strings.Join(fields[2:], " ")
		}
		branches = append(branches, b)
	}
	return branches
}
