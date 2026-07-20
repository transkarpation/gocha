// Package testutil provides helpers for integration tests.
package testutil

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var dbCounter atomic.Int64

// MongoDB connects to the test MongoDB (MONGO_URI env or the local
// docker-compose instance) and returns a unique throwaway database
// that is dropped when the test finishes. Skips the test when Mongo
// is not reachable.
func MongoDB(t *testing.T) *mongo.Database {
	t.Helper()

	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Skipf("mongo not available: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(context.Background())
		t.Skipf("mongo not reachable (start it with `docker compose up -d`): %v", err)
	}

	name := fmt.Sprintf("test_%d_%d", time.Now().UnixNano(), dbCounter.Add(1))
	db := client.Database(name)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := db.Drop(ctx); err != nil {
			t.Errorf("drop test db %s: %v", name, err)
		}
		client.Disconnect(context.Background())
	})
	return db
}
