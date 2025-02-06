package src

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var lastProcessedID primitive.ObjectID // 쿼리 조건에 사용될 ObjectId

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

func ProcessDocuments(ctx context.Context, collection *mongo.Collection, archiveField string, ageDuration time.Duration, s3Bucket, logFile string, queryLimit int64, isInit int) []map[string]interface{} {
	cutoffTime := time.Now().Add(-ageDuration)

	// 최초 수행 시에는 날짜 조건만 담는다.
	filter := bson.M{
		archiveField: bson.M{"$lt": cutoffTime},
	}

	// s3에 파일을 업로드한 이후 lastProcessedID 값을 초기화한다.
	// archiveField와 lastProcessedID 조합 조건으로 건너뛴 도큐먼트를 archiveField 조건으로만 재검색하기 위함이다.
	if isInit == 1 {
		lastProcessedID = primitive.NilObjectID // lastProcessedID 초기화
	}

	// 이전에 처리한 마지막 _id 값보다 큰 _id를 얻는다.
	if !lastProcessedID.IsZero() {
		log.Printf("startProcessedID: %s\n", lastProcessedID)

		// 2회차부터 '{archiveField:{$lt:ISODate()}}' 필터 조건에 '{_id:{$gt:ObjectId()}}' 조건을 추가
		filter["_id"] = bson.M{"$gt": lastProcessedID}
	}

	findOptions := options.Find().
		SetSort(bson.D{
			{"_id", 1},
			{archiveField, 1},
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

	// 수집한 도큐먼트의 마지막 _id를 캐싱해두고, 다음 회차에 사용한다.
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
