package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseRole(t *testing.T) {
	tests := []struct {
		input    string
		expected Role
		ok       bool
	}{
		{"viewer", RoleViewer, true},
		{"VIEWER", RoleViewer, true},
		{"  viewer  ", RoleViewer, true},
		{"editor", RoleEditor, true},
		{"ADMIN", RoleAdmin, true},
		{"unknown", RoleUnknown, false},
		{"", RoleUnknown, false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			role, ok := ParseRole(tc.input)
			assert.Equal(t, tc.expected, role)
			assert.Equal(t, tc.ok, ok)
		})
	}
}

func TestRoleHierarchy(t *testing.T) {
	assert.True(t, RoleViewer.HasPermission(RoleViewer))
	assert.False(t, RoleViewer.HasPermission(RoleEditor))
	assert.False(t, RoleViewer.HasPermission(RoleAdmin))

	assert.True(t, RoleEditor.HasPermission(RoleViewer))
	assert.True(t, RoleEditor.HasPermission(RoleEditor))
	assert.False(t, RoleEditor.HasPermission(RoleAdmin))

	assert.True(t, RoleAdmin.HasPermission(RoleViewer))
	assert.True(t, RoleAdmin.HasPermission(RoleEditor))
	assert.True(t, RoleAdmin.HasPermission(RoleAdmin))

	assert.False(t, RoleUnknown.HasPermission(RoleViewer))
}

// TestRoleForRequestUsesConfiguredTokens pins the only source of a role: the
// authenticator's configured token table. The removed RoleEnforcer treated the
// literal strings "admin" and "token-admin" as admin without consulting that
// table, so the legacy names must not authenticate at all.
func TestRoleForRequestUsesConfiguredTokens(t *testing.T) {
	a := New([]string{
		"configured-admin:admin",
		"configured-editor:editor",
		"configured-viewer:viewer",
	}, 0)

	for token, want := range map[string]Role{
		"configured-admin":  RoleAdmin,
		"configured-editor": RoleEditor,
		"configured-viewer": RoleViewer,
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		role, ok := a.RoleForRequest(req)
		assert.True(t, ok, "configured token %q must authenticate", token)
		assert.Equal(t, want, role, "configured token %q", token)
	}

	for _, token := range []string{"admin", "token-admin", "editor", "token-editor", "viewer", "token-viewer"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		role, ok := a.RoleForRequest(req)
		assert.False(t, ok, "literal token %q must not authenticate", token)
		assert.Equal(t, RoleUnknown, role, "literal token %q", token)
	}
}
