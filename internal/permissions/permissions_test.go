package permissions

import "testing"

func TestHas(t *testing.T) {
	tests := []struct {
		name string
		role Role
		perm Permission
		want bool
	}{
		{"admin can create chats", RoleAdmin, ChatsCreate, true},
		{"admin can delete chats", RoleAdmin, ChatsDelete, true},
		{"user can create chats", RoleUser, ChatsCreate, true},
		{"user cannot delete chats", RoleUser, ChatsDelete, false},
		{"user can send messages", RoleUser, MessagesCreate, true},
		{"user can read messages", RoleUser, MessagesRead, true},
		{"admin can delete users", RoleAdmin, UsersDelete, true},
		{"user cannot delete users", RoleUser, UsersDelete, false},
		{"empty role behaves as user", "", ChatsCreate, true},
		{"empty role cannot delete chats", "", ChatsDelete, false},
		{"unknown role has nothing", "superuser", ChatsCreate, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Has(tt.role, tt.perm); got != tt.want {
				t.Errorf("Has(%q, %q) = %v, want %v", tt.role, tt.perm, got, tt.want)
			}
		})
	}
}

func TestValidRole(t *testing.T) {
	if !ValidRole(RoleAdmin) || !ValidRole(RoleUser) {
		t.Error("admin and user must be valid roles")
	}
	if ValidRole("") || ValidRole("superuser") {
		t.Error("empty and unknown roles must be invalid")
	}
}
