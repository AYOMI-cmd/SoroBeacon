package auth

import "strings"

type Role int

const (
	RoleUnknown Role = iota
	RoleViewer
	RoleEditor
	RoleAdmin
)

func ParseRole(s string) (Role, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "viewer":
		return RoleViewer, true
	case "editor":
		return RoleEditor, true
	case "admin":
		return RoleAdmin, true
	default:
		return RoleUnknown, false
	}
}

func (r Role) AtLeast(required Role) bool {
	rank := func(rol Role) int {
		switch rol {
		case RoleViewer:
			return 1
		case RoleEditor:
			return 2
		case RoleAdmin:
			return 3
		default:
			return 0
		}
	}
	return rank(r) >= rank(required)
}

func (r Role) HasPermission(required Role) bool {
	return r.AtLeast(required)
}
