package types

import (
	"regexp"
	"slices"
	"strings"
)

var mentionRe = regexp.MustCompile(`(?i)(?:^|[\s(])@([a-z0-9_][a-z0-9_-]{0,29})\b`)

// ParseMentions extracts @name mentions and whether @all appears.
func ParseMentions(content string) ([]string, bool) {
	matches := mentionRe.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil, strings.Contains(strings.ToLower(content), "@all")
	}

	mentions := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	mentionAll := false
	for _, m := range matches {
		name := strings.TrimSpace(m[1])
		if name == "" {
			continue
		}
		if strings.EqualFold(name, "all") {
			mentionAll = true
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		mentions = append(mentions, name)
	}
	return mentions, mentionAll
}

func Mentioned(mentions []string, name string) bool {
	for _, mention := range mentions {
		if strings.EqualFold(mention, name) {
			return true
		}
	}
	return false
}

func NormalizeMentions(mentions []string) []string {
	if len(mentions) == 0 {
		return nil
	}
	out := make([]string, 0, len(mentions))
	seen := make(map[string]struct{}, len(mentions))
	for _, mention := range mentions {
		mention = strings.TrimSpace(mention)
		if mention == "" || strings.EqualFold(mention, "all") {
			continue
		}
		key := strings.ToLower(mention)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, mention)
	}
	slices.SortFunc(out, func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	return out
}
