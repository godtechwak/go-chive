package src

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var lastProcessedID primitive.ObjectID  // N회차 쿼리 조건에 사용될 ObjectId
var lastArchiveField primitive.DateTime // N회차 쿼리 조건에 사용될 ArchiveField

func SaveToLocalFile(filePath string, docs []map[string]interface{}) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Log full data for debugging
	log.Printf("Saving documents to file: %s", filePath)

	/* 바이너리 형태(BSON)로 저장하면 도큐먼트의 필드값이 NaN인 경우를 대비할 수 있다.

		// 역직렬화 시 아래 파이썬 코드 사용
		from bson import decode_all

		bson_file_path = "/tmp/archive_1234567890.bson"  # 실제 파일명으로 변경

		with open(bson_file_path, "rb") as f:
	    	data = f.read()

		docs_wrapper = decode_all(data)[0]
		docs = docs_wrapper["docs"]

		for doc in docs:
	    	print(doc)
	*/
	wrapper := map[string]interface{}{"docs": docs}
	data, err := bson.Marshal(wrapper)
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	return err
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
		lastProcessedID = primitive.NilObjectID                       // lastProcessedID 초기화
		lastArchiveField = primitive.NewDateTimeFromTime(time.Time{}) // lastArchiveField 초기화
	}

	// 이전에 처리한 마지막 _id 값보다 큰 _id를 얻는다.
	if !lastProcessedID.IsZero() {
		log.Printf("startProcessedID: %s\n", lastProcessedID)

		// 2회차부터 '{cutoffTime:{$lt:ISODate()}}' 필터 조건에 '{lastProcessedID:{$gt:ObjectId()}}' & '{lastArchiveField:{$gte:ISODate()}}' 조건 추가
		filter["_id"] = bson.M{"$gt": lastProcessedID}
		filter[archiveField] = bson.M{"$gte": lastArchiveField, "$lt": cutoffTime}
	}

	findOptions := options.Find().
		SetSort(bson.D{
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

	// 수집한 도큐먼트의 마지막 _id와 archiveField 값을 캐싱해두고, 다음 회차에 사용한다.
	lastProcessedID = docs[len(docs)-1]["_id"].(primitive.ObjectID)
	lastArchiveField = docs[len(docs)-1][archiveField].(primitive.DateTime)

	log.Printf("lastProcessedID: %s\n", lastProcessedID)
	log.Printf("lastArchiveField: %s\n", lastArchiveField.Time())

	filePath := fmt.Sprintf("/tmp/archive_%d.bson", time.Now().Unix())
	if err := SaveToLocalFile(filePath, docs); err != nil {
		log.Printf("Failed to save documents to file: %v", err)
		return nil
	}

	log.Printf("Archived %d documents to %s", len(docs), filePath)
	return docs
}
