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

// Roles returns every registered role. Callers that need to enumerate them
// (the API's `oneof` validation tag, docs, tests) read them from here
// rather than repeating the list, so adding a role has one obvious place
// to start and the copies that missed it fail loudly.
func Roles() []Role {
	return []Role{RoleAdmin, RoleUser}
}

type Permission string

// Chats.
const (
	ChatsCreate Permission = "chats:create"
	ChatsRead   Permission = "chats:read"
	// ChatsUpdate lets one change a chat one owns; ownership itself is
	// checked by the handler, since a permission cannot express "mine".
	ChatsUpdate Permission = "chats:update"
	// ChatsModerate is the override for acting on chats one does NOT own.
	// Handlers pair it with an ownership check: creator OR moderator.
	ChatsModerate Permission = "chats:moderate"
	ChatsDelete   Permission = "chats:delete"
)

// Messages.
const (
	MessagesCreate Permission = "messages:create"
	MessagesRead   Permission = "messages:read"
)

// Users.
const (
	UsersRead   Permission = "users:read"
	UsersUpdate Permission = "users:update"
	UsersDelete Permission = "users:delete"
	// UsersDirectory is the pared-down listing every user may read (id,
	// display name, email) so they can pick chat participants. UsersRead
	// stays admin-only: it exposes roles and lists the whole account.
	UsersDirectory Permission = "users:directory"
)

var rolePermissions = map[Role]map[Permission]bool{
	RoleAdmin: {
		ChatsCreate:    true,
		ChatsRead:      true,
		ChatsUpdate:    true,
		ChatsModerate:  true,
		ChatsDelete:    true,
		MessagesCreate: true,
		MessagesRead:   true,
		UsersRead:      true,
		UsersUpdate:    true,
		UsersDelete:    true,
		UsersDirectory: true,
	},
	RoleUser: {
		ChatsCreate:    true,
		ChatsRead:      true,
		// Coarse gate only — the handler still requires that the chat is
		// this user's own. ChatsModerate is what lifts that, admin-only.
		ChatsUpdate:    true,
		MessagesCreate: true,
		MessagesRead:   true,
		UsersDirectory: true,
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
