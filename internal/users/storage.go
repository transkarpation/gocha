package users

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"

	"github.com/transkarpation/gocha/internal/permissions"
)

var (
	ErrEmailTaken = errors.New("email already registered")
	ErrNotFound   = errors.New("not found")
	ErrNotDeleted = errors.New("user is not deleted")
)

type User struct {
	ID           bson.ObjectID    `bson:"_id,omitempty"`
	Email        string           `bson:"email"`
	PasswordHash string           `bson:"password_hash"`
	Role         permissions.Role `bson:"role"`
	CreatedAt    time.Time        `bson:"created_at"`
	DeletedAt    *time.Time       `bson:"deleted_at,omitempty"`
}

// notDeleted excludes soft-deleted users; alive users (including legacy
// documents) have no deleted_at field at all.
var notDeleted = bson.E{Key: "deleted_at", Value: bson.D{{Key: "$exists", Value: false}}}

type Session struct {
	Token     string        `bson:"token"`
	UserID    bson.ObjectID `bson:"user_id"`
	ExpiresAt time.Time     `bson:"expires_at"`
}

// ChatCredentials are the XMPP credentials of a user's mirrored chat
// account, captured from the chat backend at mirroring time. They live in
// their own collection (keyed by user id) rather than on the User document:
// they are backend-specific, exist only when mirroring succeeded, and must
// not travel with User values through handlers and listings.
type ChatCredentials struct {
	UserID       bson.ObjectID `bson:"_id"`
	XMPPUsername string        `bson:"xmpp_username"`
	XMPPPassword string        `bson:"xmpp_password"`
	UpdatedAt    time.Time     `bson:"updated_at"`
}

type Storage struct {
	users     *mongo.Collection
	sessions  *mongo.Collection
	chatCreds *mongo.Collection
}

func NewStorage(ctx context.Context, db *mongo.Database) (*Storage, error) {
	s := &Storage{
		users:     db.Collection("users"),
		sessions:  db.Collection("sessions"),
		chatCreds: db.Collection("chat_credentials"),
	}

	_, err := s.users.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "email", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	if err != nil {
		return nil, err
	}

	// TTL index: Mongo removes expired sessions automatically.
	_, err = s.sessions.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "expires_at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(0),
	})
	if err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Storage) CreateUser(ctx context.Context, email, passwordHash string, role permissions.Role) (User, error) {
	u := User{
		Email:        email,
		PasswordHash: passwordHash,
		Role:         role,
		CreatedAt:    time.Now().UTC(),
	}
	res, err := s.users.InsertOne(ctx, u)
	if mongo.IsDuplicateKeyError(err) {
		return User{}, ErrEmailTaken
	}
	if err != nil {
		return User{}, err
	}
	u.ID = res.InsertedID.(bson.ObjectID)
	return u, nil
}

func (s *Storage) UserByEmail(ctx context.Context, email string) (User, error) {
	return s.findUser(ctx, bson.D{{Key: "email", Value: email}, notDeleted})
}

func (s *Storage) UserByID(ctx context.Context, id bson.ObjectID) (User, error) {
	return s.findUser(ctx, bson.D{{Key: "_id", Value: id}, notDeleted})
}

// AnyUserByEmail and AnyUserByID also find soft-deleted users —
// gochactrl needs them so `delete --hard` can purge a soft-deleted user.
func (s *Storage) AnyUserByEmail(ctx context.Context, email string) (User, error) {
	return s.findUser(ctx, bson.D{{Key: "email", Value: email}})
}

func (s *Storage) AnyUserByID(ctx context.Context, id bson.ObjectID) (User, error) {
	return s.findUser(ctx, bson.D{{Key: "_id", Value: id}})
}

func (s *Storage) findUser(ctx context.Context, filter bson.D) (User, error) {
	var u User
	err := s.users.FindOne(ctx, filter).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return User{}, ErrNotFound
	}
	// Users created before roles existed have no role field.
	if u.Role == "" {
		u.Role = permissions.RoleUser
	}
	return u, err
}

// ListUsers returns users ordered by creation time (oldest first).
func (s *Storage) ListUsers(ctx context.Context, limit, offset int64) ([]User, error) {
	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: 1}}).
		SetLimit(limit).
		SetSkip(offset)
	cur, err := s.users.Find(ctx, bson.D{notDeleted}, opts)
	if err != nil {
		return nil, err
	}
	list := []User{}
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Role == "" {
			list[i].Role = permissions.RoleUser
		}
	}
	return list, nil
}

// UserUpdate lists the fields UpdateUser changes; nil fields are kept as is.
type UserUpdate struct {
	Email        *string
	PasswordHash *string
	Role         *permissions.Role
}

// UpdateUser applies the partial update and returns the updated user.
func (s *Storage) UpdateUser(ctx context.Context, id bson.ObjectID, upd UserUpdate) (User, error) {
	set := bson.D{}
	if upd.Email != nil {
		set = append(set, bson.E{Key: "email", Value: *upd.Email})
	}
	if upd.PasswordHash != nil {
		set = append(set, bson.E{Key: "password_hash", Value: *upd.PasswordHash})
	}
	if upd.Role != nil {
		set = append(set, bson.E{Key: "role", Value: *upd.Role})
	}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var u User
	err := s.users.FindOneAndUpdate(ctx,
		bson.D{{Key: "_id", Value: id}, notDeleted},
		bson.D{{Key: "$set", Value: set}},
		opts,
	).Decode(&u)
	switch {
	case errors.Is(err, mongo.ErrNoDocuments):
		return User{}, ErrNotFound
	case mongo.IsDuplicateKeyError(err):
		return User{}, ErrEmailTaken
	case err != nil:
		return User{}, err
	}
	if u.Role == "" {
		u.Role = permissions.RoleUser
	}
	return u, nil
}

// SoftDeleteUser marks the user as deleted (deleted_at) and invalidates
// their sessions. The document — and its email — stay in the collection,
// so re-registering the same email keeps failing with ErrEmailTaken.
func (s *Storage) SoftDeleteUser(ctx context.Context, id bson.ObjectID) error {
	res, err := s.users.UpdateOne(ctx,
		bson.D{{Key: "_id", Value: id}, notDeleted},
		bson.D{{Key: "$set", Value: bson.D{{Key: "deleted_at", Value: time.Now().UTC()}}}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrNotFound
	}
	return s.DeleteSessions(ctx, id)
}

// RestoreUser clears deleted_at on a soft-deleted user and returns the
// restored user. Restoring an alive user reports ErrNotDeleted.
func (s *Storage) RestoreUser(ctx context.Context, id bson.ObjectID) (User, error) {
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
	var u User
	err := s.users.FindOneAndUpdate(ctx,
		bson.D{
			{Key: "_id", Value: id},
			{Key: "deleted_at", Value: bson.D{{Key: "$exists", Value: true}}},
		},
		bson.D{{Key: "$unset", Value: bson.D{{Key: "deleted_at", Value: ""}}}},
		opts,
	).Decode(&u)
	if errors.Is(err, mongo.ErrNoDocuments) {
		// Distinguish "no such user" from "user is alive".
		if _, lookupErr := s.AnyUserByID(ctx, id); lookupErr == nil {
			return User{}, ErrNotDeleted
		}
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	if u.Role == "" {
		u.Role = permissions.RoleUser
	}
	return u, nil
}

// DeleteUser permanently removes the user, all their sessions (so
// outstanding tokens stop working immediately) and their stored chat
// credentials.
func (s *Storage) DeleteUser(ctx context.Context, id bson.ObjectID) error {
	res, err := s.users.DeleteOne(ctx, bson.D{{Key: "_id", Value: id}})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	if err := s.DeleteChatCredentials(ctx, id); err != nil {
		return err
	}
	return s.DeleteSessions(ctx, id)
}

// AllUserIDs returns the ids of every user.
func (s *Storage) AllUserIDs(ctx context.Context) ([]bson.ObjectID, error) {
	opts := options.Find().SetProjection(bson.D{{Key: "_id", Value: 1}})
	cur, err := s.users.Find(ctx, bson.D{}, opts)
	if err != nil {
		return nil, err
	}
	var docs []struct {
		ID bson.ObjectID `bson:"_id"`
	}
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	ids := make([]bson.ObjectID, len(docs))
	for i, d := range docs {
		ids[i] = d.ID
	}
	return ids, nil
}

// DeleteAllUsers removes every user, every session and all stored chat
// credentials — except the system account, which the server depends on:
// it survives together with its sessions and chat credentials.
func (s *Storage) DeleteAllUsers(ctx context.Context) (int64, error) {
	userFilter, sessFilter, credFilter := bson.D{}, bson.D{}, bson.D{}
	sys, err := s.AnyUserByEmail(ctx, SystemEmail)
	switch {
	case err == nil:
		userFilter = bson.D{{Key: "_id", Value: bson.D{{Key: "$ne", Value: sys.ID}}}}
		sessFilter = bson.D{{Key: "user_id", Value: bson.D{{Key: "$ne", Value: sys.ID}}}}
		credFilter = bson.D{{Key: "_id", Value: bson.D{{Key: "$ne", Value: sys.ID}}}}
	case !errors.Is(err, ErrNotFound):
		return 0, err
	}

	res, err := s.users.DeleteMany(ctx, userFilter)
	if err != nil {
		return 0, err
	}
	if _, err := s.sessions.DeleteMany(ctx, sessFilter); err != nil {
		return res.DeletedCount, err
	}
	if _, err := s.chatCreds.DeleteMany(ctx, credFilter); err != nil {
		return res.DeletedCount, err
	}
	return res.DeletedCount, nil
}

// SaveChatCredentials stores (or replaces) the XMPP credentials of the
// user's mirrored chat account.
func (s *Storage) SaveChatCredentials(ctx context.Context, userID bson.ObjectID, xmppUsername, xmppPassword string) error {
	_, err := s.chatCreds.ReplaceOne(ctx,
		bson.D{{Key: "_id", Value: userID}},
		ChatCredentials{
			UserID:       userID,
			XMPPUsername: xmppUsername,
			XMPPPassword: xmppPassword,
			UpdatedAt:    time.Now().UTC(),
		},
		options.Replace().SetUpsert(true),
	)
	return err
}

// ChatCredentialsByUserID returns the stored XMPP credentials of the user's
// mirrored chat account.
func (s *Storage) ChatCredentialsByUserID(ctx context.Context, userID bson.ObjectID) (ChatCredentials, error) {
	var c ChatCredentials
	err := s.chatCreds.FindOne(ctx, bson.D{{Key: "_id", Value: userID}}).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return ChatCredentials{}, ErrNotFound
	}
	return c, err
}

// DeleteChatCredentials removes the stored XMPP credentials of the user.
// Missing credentials are not an error — mirroring may have been disabled
// or failed when the user was registered.
func (s *Storage) DeleteChatCredentials(ctx context.Context, userID bson.ObjectID) error {
	_, err := s.chatCreds.DeleteOne(ctx, bson.D{{Key: "_id", Value: userID}})
	return err
}

// DeleteSessions invalidates all sessions of the user.
func (s *Storage) DeleteSessions(ctx context.Context, userID bson.ObjectID) error {
	_, err := s.sessions.DeleteMany(ctx, bson.D{{Key: "user_id", Value: userID}})
	return err
}

// CountExisting returns how many of the given user ids exist and are
// not soft-deleted.
func (s *Storage) CountExisting(ctx context.Context, ids []bson.ObjectID) (int64, error) {
	filter := bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}}, notDeleted}
	return s.users.CountDocuments(ctx, filter)
}

// SessionByToken returns the session only if it has not expired yet:
// the TTL monitor deletes expired documents with up to a minute of delay,
// so the expiry is checked in the query as well.
func (s *Storage) SessionByToken(ctx context.Context, token string) (Session, error) {
	filter := bson.D{
		{Key: "token", Value: token},
		{Key: "expires_at", Value: bson.D{{Key: "$gt", Value: time.Now().UTC()}}},
	}
	var sess Session
	err := s.sessions.FindOne(ctx, filter).Decode(&sess)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Session{}, ErrNotFound
	}
	return sess, err
}

func (s *Storage) CreateSession(ctx context.Context, userID bson.ObjectID, token string, ttl time.Duration) (Session, error) {
	sess := Session{
		Token:     token,
		UserID:    userID,
		ExpiresAt: time.Now().UTC().Add(ttl),
	}
	_, err := s.sessions.InsertOne(ctx, sess)
	if err != nil {
		return Session{}, err
	}
	return sess, nil
}
