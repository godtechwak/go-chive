package src

import (
	"context"
	"time"
	"fmt"
	"os"
	"log"
	"encoding/json"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var lastProcessedID primitive.ObjectID

func SaveToLocalFile(filePath string, docs []map[string]interface{}) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Log full data for debugging
	log.Printf("Saving documents to file: %s", filePath)

	encoder := json.NewEncoder(file)
	return encoder.Encode(docs)
}

func ProcessDocuments(ctx context.Context, collection *mongo.Collection, archiveField string, ageDuration time.Duration, s3Bucket, logFile string, queryLimit int64) []map[string]interface{} {
    cutoffTime := time.Now().Add(-ageDuration)

    filter := bson.M{
        archiveField: bson.M{"$lt": cutoffTime},
    }
    if !lastProcessedID.IsZero() {
        log.Printf("startProcessedID: %s\n", lastProcessedID)
        filter["_id"] = bson.M{"$gt": lastProcessedID}
    }

    findOptions := options.Find().
        SetSort(bson.D{
            {archiveField, 1},
            {"_id", 1},
        }).
        SetLimit(queryLimit)

    var docs []map[string]interface{}
    cur, err := collection.Find(ctx, filter, findOptions)
    if err != nil {
        log.Printf("Failed to query documents: %v", err)
        return nil
    }
    if err := cur.All(ctx, &docs); err != nil {
        log.Printf("Failed to decode documents: %v", err)
        return nil
    }
    cur.Close(ctx)

    if len(docs) == 0 {
        log.Println("No documents to archive.")
        return nil
    }

    lastProcessedID = docs[len(docs)-1]["_id"].(primitive.ObjectID)
    log.Printf("lastProcessedID: %s\n", lastProcessedID)

    filePath := fmt.Sprintf("/tmp/archive_%d.json", time.Now().Unix())
    if err := SaveToLocalFile(filePath, docs); err != nil {
        log.Printf("Failed to save documents to file: %v", err)
        return nil
    }

    log.Printf("Archived %d documents to %s", len(docs), filePath)
    return docs
}
