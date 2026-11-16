package store

import "strings"

// mysqlLegacySQL quotes the legacy table name groups, which was accepted by
// MariaDB but is reserved in MySQL 8.4. This is an execution-only adaptation:
// embedded bytes, filenames and their checksums remain unchanged. Only table
// positions in the frozen SQL catalog are changed, not literals/comments,
// variables, other identifiers or a SQL GROUPS window-frame keyword.
func mysqlLegacySQL(text string) (string, error) {
	mask, err := syntaxMask(text, "mysql")
	if err != nil {
		return "", err
	}
	isWord := func(b byte) bool {
		return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '$' || b >= 128
	}
	var out strings.Builder
	previous, copied := "", 0
	for i := 0; i < len(mask); {
		if !isWord(mask[i]) {
			i++
			continue
		}
		start := i
		for i < len(mask) && isWord(mask[i]) {
			i++
		}
		word := strings.ToUpper(string(mask[start:i]))
		if word == "GROUPS" {
			switch previous {
			case "TABLE", "FROM", "JOIN", "INTO", "UPDATE", "REFERENCES", "EXISTS":
				out.WriteString(text[copied:start])
				out.WriteByte('`')
				out.WriteString(text[start:i])
				out.WriteByte('`')
				copied = i
			}
		}
		previous = word
	}
	out.WriteString(text[copied:])
	return out.String(), nil
}
