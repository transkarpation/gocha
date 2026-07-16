package chats

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var ErrNotFound = errors.New("chat not found")

const (
	TypePublic = "public"
	TypeGroup  = "group"
)

type Chat struct {
	ID           bson.ObjectID   `bson:"_id,omitempty"`
	Name         string          `bson:"name"`
	Type         string          `bson:"type"`
	Participants []bson.ObjectID `bson:"participants"`
	CreatedBy    bson.ObjectID   `bson:"created_by"`
	CreatedAt    time.Time       `bson:"created_at"`
}

type Message struct {
	ID        bson.ObjectID `bson:"_id,omitempty"`
	ChatID    bson.ObjectID `bson:"chat_id"`
	AuthorID  bson.ObjectID `bson:"author_id"`
	Text      string        `bson:"text"`
	CreatedAt time.Time     `bson:"created_at"`
}

type Storage struct {
	chats    *mongo.Collection
	messages *mongo.Collection
}

func NewStorage(ctx context.Context, db *mongo.Database) (*Storage, error) {
	s := &Storage{
		chats:    db.Collection("chats"),
		messages: db.Collection("messages"),
	}
	_, err := s.messages.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "chat_id", Value: 1}, {Key: "created_at", Value: -1}},
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Storage) Create(ctx context.Context, c Chat) (Chat, error) {
	c.CreatedAt = time.Now().UTC()
	res, err := s.chats.InsertOne(ctx, c)
	if err != nil {
		return Chat{}, err
	}
	c.ID = res.InsertedID.(bson.ObjectID)
	return c, nil
}

func (s *Storage) ChatByID(ctx context.Context, id bson.ObjectID) (Chat, error) {
	var c Chat
	err := s.chats.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Chat{}, ErrNotFound
	}
	return c, err
}

func (s *Storage) CreateMessage(ctx context.Context, m Message) (Message, error) {
	m.CreatedAt = time.Now().UTC()
	res, err := s.messages.InsertOne(ctx, m)
	if err != nil {
		return Message{}, err
	}
	m.ID = res.InsertedID.(bson.ObjectID)
	return m, nil
}

// MessagesByChat returns messages newest-first.
func (s *Storage) MessagesByChat(ctx context.Context, chatID bson.ObjectID, limit, offset int64) ([]Message, error) {
	opts := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetLimit(limit).
		SetSkip(offset)
	cur, err := s.messages.Find(ctx, bson.D{{Key: "chat_id", Value: chatID}}, opts)
	if err != nil {
		return nil, err
	}
	messages := []Message{}
	if err := cur.All(ctx, &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

func (s *Storage) Delete(ctx context.Context, id bson.ObjectID) error {
	res, err := s.chats.DeleteOne(ctx, bson.D{{Key: "_id", Value: id}})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrNotFound
	}
	return nil
}
