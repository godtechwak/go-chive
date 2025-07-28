package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"go_chive/src"
	"go_chive/util"
)

var ArchiveMutex sync.Mutex

type Config struct {
	MongoURI             string
	DatabaseName         string
	CollectionName       string
	S3Bucket             string
	AgeLimit             string
	ArchiveField         string
	QueryLimit           int64
	S3UploadInterval     int
	ArchiveCheckInterval int
}

/* go-chive flow
(1) processDocuments() -> saveToLocalFile() by "archiveCheckInterval"
(2) performS3Upload() -> deleteArchivedDocs() -> delete a localfile by "s3UploadInterval"
*/

func main() {
	// 옵션 플래그
	mongoURI := flag.String("host", "", "MongoDB connection URI")
	user := flag.String("user", "", "MongoDB username")
	password := flag.String("password", "", "MongoDB password")
	databaseName := flag.String("database", "", "MongoDB database name")
	collectionName := flag.String("collection", "", "MongoDB collection name")
	archiveCheckInterval := flag.Int("archive-check-interval", 60, "Interval for checking documents to archive in seconds")
	s3UploadInterval := flag.Int("s3-upload-interval", 120, "Interval for S3 uploads in seconds")
	s3Bucket := flag.String("s3-bucket-uri", "", "S3 bucket URI")
	ageLimit := flag.String("age-limit", "45d", "Document age limit for archiving")
	logFile := flag.String("log-file", "deletion_log.txt", "File to log deleted document IDs")
	queryLimit := flag.Int64("query-limit", 100, "Limit for the number of documents to query in a single operation")
	archiveField := flag.String("archive-field", "", "Field name for filtering documents to archive")
	flag.Parse()

	/* 2025.02.25 현재 이 제한은 적용되지 않으며, 리소스 제한이 필요한 경우 사용할 수 있다.
	===================
	- 옵션 플래그 제한 조건
	===================
	newDocs slice에 append되는 최대 크기는 2048MB(2GB)로 제한(메모리 사용 제한)한다.
	이 제한은 s3Ticker 타이머가 발동되는 시점에 삭제할 수 있는 도큐먼트의 최대 크기를 결정한다.

	최대 크기 제한 산식은 다음과 같다.
	==> QueryLimit * (s3UploadInterval / archiveCheckInterval) < 2048
	(단, 1개 도큐먼트의 최대 크기를 1MB로 가정)

	ex1)
		- QueryLimit = 1200
		- s3UploadInterval = 120
		- archiveCheckInterval = 60
	==> 1200 * (120 / 60) = 2400 --> 실행 불가

	ex2)
		- QueryLimit = 100
		- s3UploadInterval = 600
		- archiveCheckInterval = 60
	==> 100 * (600 / 60) = 1000 --> 실행 가능

	ex3)
		- QueryLimit = 5000
		- s3UploadInterval = 60
		- archiveCheckInterval = 60
	==> 5000 * (60 / 60) = 5000 --> 실행 불가

	ex4)
		- QueryLimit = 10
		- s3UploadInterval = 900
		- archiveCheckInterval = 10
	==> 10 * (900 / 10) = 900 --> 실행 가능

	*/

	cfg := Config{
		MongoURI:             *mongoURI,
		DatabaseName:         *databaseName,
		CollectionName:       *collectionName,
		S3Bucket:             *s3Bucket,
		AgeLimit:             *ageLimit,
		ArchiveField:         *archiveField,
		QueryLimit:           *queryLimit,
		S3UploadInterval:     *s3UploadInterval,
		ArchiveCheckInterval: *archiveCheckInterval,
	}

	// 플래그 검증 호출
	validateFlags(cfg)

	// ageLimit을 문자열로 받아서 integer로 변환
	days, err := util.ParseDuration(cfg.AgeLimit)
	if err != nil {
		log.Fatalf("Invalid age limit: %v", err)
	}

	// 일수를 시간으로 계산
	ageDuration := time.Duration(days) * 24 * time.Hour
	ctx := context.Background()

	// 플래그에서 입력받은 값을 바탕으로 MongoDB URI 설정
	if *user != "" && *password != "" {
		cfg.MongoURI = fmt.Sprintf("mongodb+srv://%s:%s@%s", *user, *password, *mongoURI)
	}

	// MongoDB 클라이언트 객체 얻기
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	collection := client.Database(cfg.DatabaseName).Collection(cfg.CollectionName)

	// lastProcessedID 초기화 여부I(1이면 초기화)
	isInit := 0

	// 아카이빙할 도큐먼트 검색
	src.ProcessDocuments(ctx, collection, cfg.ArchiveField, ageDuration, cfg.S3Bucket, *logFile, cfg.QueryLimit, isInit)

	// 아카이브 타이머 생성
	ticker := time.NewTicker(time.Duration(cfg.ArchiveCheckInterval) * time.Second)
	defer ticker.Stop()

	// S3 업로드 타이머 생성
	s3Ticker := time.NewTicker(time.Duration(cfg.S3UploadInterval) * time.Second)
	defer s3Ticker.Stop()

	var newDocs []map[string]interface{} // 신규로 조회한 도큐먼트
	var docs []map[string]interface{}    // 누적되는 도큐먼트

	for {
		// ticker.C와 s3Ticker 타이머의 시간이 겹칠 때는 1개 타이머만 동작하도록 뮤텍스 잠금을 건다.
		select {
		case <-ticker.C:
			ArchiveMutex.Lock()
			newDocs = src.ProcessDocuments(ctx, collection, cfg.ArchiveField, ageDuration, cfg.S3Bucket, *logFile, cfg.QueryLimit, isInit)

			/*
				1.
				S3 업로드 타이머가 동작하기 전까지 docs 맵 배열에 아카이빙 대상 도큐먼트를 추가한다.
			*/
			if len(newDocs) > 0 {
				docs = append(docs, newDocs...)
				log.Println("Documents ready for archiving.")
				isInit = 0
			}
			ArchiveMutex.Unlock()
		case <-s3Ticker.C:
			ArchiveMutex.Lock()
			fmt.Printf("%d\n", len(docs))

			/*
				2.
				docs 배열에 데이터가 존재하면, /tmp 경로에 생성된 bson 파일을 압축하고 S3 업로드를 수행한다.
				업로드가 완료된 이후에는 실제 물리적인 도큐먼트를 삭제한다.
			*/

			if len(docs) > 0 {
				src.PerformS3Upload(cfg.S3Bucket, ctx, collection, docs, *logFile)
				docs = nil
				isInit = 1 // 수행 회차 초기화 여부. lastProcessedID를 초기화하여 중간에 건너뛴 도큐먼트를 처리한다.
			}
			ArchiveMutex.Unlock()
		}
	}
}

func validateFlags(cfg Config) {
	calculatedSize := cfg.QueryLimit * int64(cfg.S3UploadInterval) / int64(cfg.ArchiveCheckInterval)
	if calculatedSize >= 100000 {
		log.Fatalf("Invalid configuration: QueryLimit * (S3UploadInterval / ArchiveCheckInterval) must be less than 2048. Current value: %d", calculatedSize)
	}

	// S3 업로드 타이머는 아카이브 타이머보다 수행 주기가 길어야 한다.
	if cfg.S3UploadInterval <= cfg.ArchiveCheckInterval {
		log.Fatalf("Invalid configuration: archiveCheckInterval flag must be less than S3UploadInterval.\n\t- Current value: archiveCheckInterval(%d)/S3UploadInterval(%d)", cfg.ArchiveCheckInterval, cfg.S3UploadInterval)
	}

	if cfg.MongoURI == "" || cfg.DatabaseName == "" || cfg.CollectionName == "" || cfg.S3Bucket == "" || cfg.AgeLimit == "" || cfg.ArchiveField == "" {
		log.Fatal("All flags must be provided\n\t-host\n\t-user\n\t-password\n\t-database\n\t-collection\n\t-archive-check-interval\n\t-s3-bucket-uri\n")
	}
}
