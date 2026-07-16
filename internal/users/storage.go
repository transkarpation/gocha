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
)

type User struct {
	ID           bson.ObjectID    `bson:"_id,omitempty"`
	Email        string           `bson:"email"`
	PasswordHash string           `bson:"password_hash"`
	Role         permissions.Role `bson:"role"`
	CreatedAt    time.Time        `bson:"created_at"`
}

type Session struct {
	Token     string        `bson:"token"`
	UserID    bson.ObjectID `bson:"user_id"`
	ExpiresAt time.Time     `bson:"expires_at"`
}

type Storage struct {
	users    *mongo.Collection
	sessions *mongo.Collection
}

func NewStorage(ctx context.Context, db *mongo.Database) (*Storage, error) {
	s := &Storage{
		users:    db.Collection("users"),
		sessions: db.Collection("sessions"),
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
	return s.findUser(ctx, bson.D{{Key: "email", Value: email}})
}

func (s *Storage) UserByID(ctx context.Context, id bson.ObjectID) (User, error) {
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
	cur, err := s.users.Find(ctx, bson.D{}, opts)
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
		bson.D{{Key: "_id", Value: id}},
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

// DeleteUser removes the user and all their sessions, so outstanding
// tokens stop working immediately.
func (s *Storage) DeleteUser(ctx context.Context, id bson.ObjectID) error {
	res, err := s.users.DeleteOne(ctx, bson.D{{Key: "_id", Value: id}})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return s.DeleteSessions(ctx, id)
}

// DeleteSessions invalidates all sessions of the user.
func (s *Storage) DeleteSessions(ctx context.Context, userID bson.ObjectID) error {
	_, err := s.sessions.DeleteMany(ctx, bson.D{{Key: "user_id", Value: userID}})
	return err
}

// CountExisting returns how many of the given user ids exist.
func (s *Storage) CountExisting(ctx context.Context, ids []bson.ObjectID) (int64, error) {
	filter := bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: ids}}}}
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
