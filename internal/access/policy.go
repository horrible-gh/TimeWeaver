package access

import (
	"fmt"
	"sort"
	"strings"
)

type Error struct {
	Status int
	Code   string
}

func (e *Error) Error() string               { return e.Code }
func failure(status int, code string) *Error { return &Error{status, code} }
func unavailable() *Error                    { return failure(503, "unavailable") }

type Membership struct {
	GroupID int64  `json:"group_id"`
	Role    string `json:"role"`
}
type User struct {
	ID                 string       `json:"user_id"`
	Name               string       `json:"name"`
	Role               string       `json:"role"`
	GroupID            int64        `json:"group_id"`
	Groups             []Membership `json:"groups"`
	MustChangePassword bool         `json:"must_change_password"`
	AuthMethod         string       `json:"auth_method"`
	OSName             string       `json:"os_name,omitempty"`
	version            int64
	password, status   string
	osSubject          string
}
type Principal struct {
	User    User   `json:"user"`
	CSRF    string `json:"csrf_token"`
	session string
}

func rank(role string) int {
	switch role {
	case "viewer":
		return 1
	case "operator":
		return 2
	case "editor":
		return 3
	}
	return 0
}

// Group zero is the explicit global membership; it is not an implicit default.
func (u User) Can(group int64, required string) bool {
	need := rank(required)
	if group < 0 || need == 0 || u.MustChangePassword || u.status != "active" {
		return false
	}
	if u.Role == "admin" {
		return true
	}
	if u.Role != "user" {
		return false
	}
	for _, m := range u.Groups {
		if (m.GroupID == 0 || m.GroupID == group) && rank(m.Role) >= need {
			return true
		}
	}
	return false
}

// GroupFilter supplies a parameterized scope for future dashboard queries.
// column MUST be a code-owned identifier, never a user-provided SQL expression.
func (u User) GroupFilter(column, required string) (string, []any, error) {
	if column == "" {
		return "", nil, fmt.Errorf("invalid scope column")
	}
	for _, c := range column {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.') {
			return "", nil, fmt.Errorf("invalid scope column")
		}
	}
	if rank(required) == 0 {
		return "", nil, fmt.Errorf("invalid scope role")
	}
	if u.Can(0, required) {
		return "1=1", nil, nil
	}
	ids := []int64{}
	seen := map[int64]bool{}
	for _, m := range u.Groups {
		if m.GroupID > 0 && !seen[m.GroupID] && u.Can(m.GroupID, required) {
			seen[m.GroupID] = true
			ids = append(ids, m.GroupID)
		}
	}
	if len(ids) == 0 {
		return "1=0", nil, nil
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	args := make([]any, len(ids))
	for i, v := range ids {
		args[i] = v
	}
	return column + " IN (" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")", args, nil
}
