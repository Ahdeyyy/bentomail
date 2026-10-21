package mailclient

import (
	"strings"
	"time"
)

type searchFilter struct {
	field    string // "", "from", "to", "subject", "tag", "category", "account", "folder", "attachment", "has", "is", "before", "after"
	val      string
	negative bool
}

// tokenizeQuery splits query into tokens, preserving quoted substrings.
func tokenizeQuery(q string) []string {
	var tokens []string
	var current strings.Builder
	inQuotes := false

	runes := []rune(q)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '"' {
			inQuotes = !inQuotes
			current.WriteRune(r)
		} else if (r == ' ' || r == '\t') && !inQuotes {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		} else {
			current.WriteRune(r)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func parseFilters(query string) []searchFilter {
	tokens := tokenizeQuery(query)
	var filters []searchFilter

	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}

		neg := false
		if strings.HasPrefix(token, "-") {
			neg = true
			token = strings.TrimPrefix(token, "-")
		}

		// Strip quotes if whole token is quoted
		if strings.HasPrefix(token, "\"") && strings.HasSuffix(token, "\"") && len(token) >= 2 {
			token = token[1 : len(token)-1]
			filters = append(filters, searchFilter{field: "", val: token, negative: neg})
			continue
		}

		colonIdx := strings.Index(token, ":")
		if colonIdx > 0 {
			field := strings.ToLower(token[:colonIdx])
			val := token[colonIdx+1:]
			val = strings.Trim(val, "\"")
			filters = append(filters, searchFilter{field: field, val: val, negative: neg})
		} else {
			filters = append(filters, searchFilter{field: "", val: token, negative: neg})
		}
	}
	return filters
}

// Search filters messages matching the search query with support for operators and exclusions.
func Search(messages []Message, query string) []Message {
	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return messages
	}

	filters := parseFilters(trimmed)
	var results []Message

	for _, m := range messages {
		if matchMessage(m, filters) {
			results = append(results, m)
		}
	}
	return results
}

func matchMessage(m Message, filters []searchFilter) bool {
	for _, f := range filters {
		matched := evalFilter(m, f)
		if f.negative {
			if matched {
				return false
			}
		} else {
			if !matched {
				return false
			}
		}
	}
	return true
}

func evalFilter(m Message, f searchFilter) bool {
	vLower := strings.ToLower(f.val)

	switch f.field {
	case "from":
		return strings.Contains(strings.ToLower(m.From), vLower)
	case "to":
		return strings.Contains(strings.ToLower(m.To), vLower) || strings.Contains(strings.ToLower(m.CC), vLower)
	case "subject":
		return strings.Contains(strings.ToLower(m.Subject), vLower)
	case "tag":
		for _, t := range m.Tags {
			if strings.EqualFold(t, f.val) {
				return true
			}
		}
		return false
	case "category":
		return strings.Contains(strings.ToLower(m.Category), vLower)
	case "account":
		return strings.EqualFold(m.AccountID, f.val)
	case "folder":
		return strings.EqualFold(m.Folder, f.val)
	case "attachment":
		for _, att := range m.Attachments {
			if strings.Contains(strings.ToLower(att.Name), vLower) {
				return true
			}
		}
		return false
	case "has":
		if vLower == "attachment" || vLower == "attachments" {
			return len(m.Attachments) > 0
		}
		return false
	case "is":
		switch vLower {
		case "read":
			return m.Read
		case "unread":
			return !m.Read
		case "starred", "star":
			return m.Starred
		case "unstarred":
			return !m.Starred
		case "important":
			return m.Important
		case "snoozed":
			return !m.SnoozedUntil.IsZero() && m.SnoozedUntil.After(time.Now())
		}
		return false
	case "before":
		t, err := time.Parse("2006-01-02", f.val)
		if err == nil {
			return m.Date.Before(t)
		}
		return false
	case "after":
		t, err := time.Parse("2006-01-02", f.val)
		if err == nil {
			return m.Date.After(t)
		}
		return false
	default:
		// Free text search in subject, from, to, cc, text, html
		return strings.Contains(strings.ToLower(m.Subject), vLower) ||
			strings.Contains(strings.ToLower(m.From), vLower) ||
			strings.Contains(strings.ToLower(m.To), vLower) ||
			strings.Contains(strings.ToLower(m.CC), vLower) ||
			strings.Contains(strings.ToLower(m.Text), vLower) ||
			strings.Contains(strings.ToLower(m.HTML), vLower)
	}
}
