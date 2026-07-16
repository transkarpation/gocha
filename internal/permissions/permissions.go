// Package permissions is the single registry of roles and permissions.
// Every entity exposed over HTTP must define its permission set here
// and guard its routes with users.RequirePermission.
package permissions

type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

func ValidRole(r Role) bool {
	return r == RoleAdmin || r == RoleUser
}

type Permission string

// Chats.
const (
	ChatsCreate Permission = "chats:create"
	ChatsRead   Permission = "chats:read"
	ChatsUpdate Permission = "chats:update"
	ChatsDelete Permission = "chats:delete"
)

// Messages.
const (
	MessagesCreate Permission = "messages:create"
	MessagesRead   Permission = "messages:read"
)

// Users.
const (
	UsersRead   Permission = "users:read"
	UsersDelete Permission = "users:delete"
)

var rolePermissions = map[Role]map[Permission]bool{
	RoleAdmin: {
		ChatsCreate:    true,
		ChatsRead:      true,
		ChatsUpdate:    true,
		ChatsDelete:    true,
		MessagesCreate: true,
		MessagesRead:   true,
		UsersRead:      true,
		UsersDelete:    true,
	},
	RoleUser: {
		ChatsCreate:    true,
		ChatsRead:      true,
		MessagesCreate: true,
		MessagesRead:   true,
	},
}

// Has reports whether the role is allowed the permission.
// An empty role (users created before roles existed) counts as RoleUser.
func Has(r Role, p Permission) bool {
	if r == "" {
		r = RoleUser
	}
	return rolePermissions[r][p]
}
