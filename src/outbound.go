package src

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// S3에 도큐먼트 아카이빙
func PerformS3Upload(bucket string, ctx context.Context, collection *mongo.Collection, docs []map[string]interface{}, logFile string) {
	files, err := ioutil.ReadDir("/tmp")
	if err != nil {
		log.Printf("Failed to read directory: %v", err)
		return
	}

	// 타임스탬프 기반 tar.gz 파일명 생성
	timestamp := time.Now().Format("20060102_150405")
	archiveName := fmt.Sprintf("archive_%s.tar.gz", timestamp)

	// JSON 파일 압축
	err = createTarGz("/tmp/"+archiveName, files)
	if err != nil {
		log.Printf("Failed to create tar.gz file: %v", err)
		return
	}

	// AWS 세션 생성
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String("ap-northeast-2"),
	})
	if err != nil {
		log.Printf("Failed to create AWS session: %v", err)
		return
	}

	uploader := s3.New(sess)

	// tar.gz 파일 업로드
	fileHandle, err := os.Open("/tmp/" + archiveName)
	if err != nil {
		log.Printf("Failed to open tar.gz file %s: %v", archiveName, err)
		return
	}
	defer fileHandle.Close()

	key := fmt.Sprintf("%s_%s", collection.Name(), archiveName)
	_, err = uploader.PutObject(&s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   fileHandle,
	})
	if err != nil {
		log.Printf("Failed to upload tar.gz file %s to S3: %v", archiveName, err)
		return
	}

	log.Printf("Successfully uploaded tar.gz file to S3: %s/%s", bucket, key)

	// 도큐먼트 삭제
	err = DeleteArchivedDocs(ctx, collection, docs, logFile)
	if err != nil {
		log.Printf("Error deleting archived documents: %v", err)
	}

	// 도큐먼트 삭제되었는지 검증
	VerifyDeletion(ctx, collection, docs)

	// JSON 파일과 tar.gz 파일 삭제
	deleteLocalFiles(files, archiveName)
}

// JSON 파일들을 tar.gz로 압축
func createTarGz(outputName string, files []os.FileInfo) error {
	outFile, err := os.Create(outputName)
	if err != nil {
		return fmt.Errorf("failed to create tar.gz file: %w", err)
	}
	defer outFile.Close()

	gzipWriter := gzip.NewWriter(outFile)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	for _, file := range files {
		if filepath.Ext(file.Name()) != ".json" {
			continue
		}

		fileHandle, err := os.Open("/tmp/" + file.Name())
		if err != nil {
			log.Printf("Failed to open file %s: %v", file.Name(), err)
			continue
		}
		defer fileHandle.Close()

		// 파일 정보 작성
		stat, err := fileHandle.Stat()
		if err != nil {
			log.Printf("Failed to get file stats for %s: %v", file.Name(), err)
			continue
		}

		header := &tar.Header{
			Name: file.Name(),
			Size: stat.Size(),
			Mode: int64(stat.Mode()),
		}

		if err := tarWriter.WriteHeader(header); err != nil {
			log.Printf("Failed to write header for file %s: %v", file.Name(), err)
			continue
		}

		// 파일 내용 쓰기
		if _, err := io.Copy(tarWriter, fileHandle); err != nil {
			log.Printf("Failed to write file %s to tar: %v", file.Name(), err)
			continue
		}
	}

	return nil
}

// S3에 아카이빙한 이후 도큐먼트 삭제
func DeleteArchivedDocs(ctx context.Context, collection *mongo.Collection, docs []map[string]interface{}, logFile string) error {
	logFileHandle, err := os.OpenFile("/tmp/"+logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer logFileHandle.Close()

	for _, doc := range docs {
		rawID, ok := doc["_id"]
		if !ok {
			log.Printf("Document does not have an '_id' field: %v", doc)
			continue
		}

		var objectID primitive.ObjectID

		// `_id` 필드 타입 확인 및 변환
		switch id := rawID.(type) {
		case primitive.ObjectID:
			objectID = id
		case string:
			// 문자열 타입이면 ObjectID로 변환 시도
			parsedID, err := primitive.ObjectIDFromHex(id)
			if err != nil {
				log.Printf("Invalid ObjectID format for ID: %s, error: %v", id, err)
				continue
			}
			objectID = parsedID
		default:
			log.Printf("Unsupported '_id' type: %T for document: %v", id, doc)
			continue
		}

		// MongoDB에서 문서 삭제
		res, err := collection.DeleteOne(ctx, bson.M{"_id": objectID})
		if err != nil {
			return fmt.Errorf("failed to delete document %s: %w", objectID.Hex(), err)
		}
		if res.DeletedCount == 0 {
			log.Printf("No documents deleted for ID: %s", objectID.Hex())
		}
		//else {
		//	logFileHandle.WriteString(fmt.Sprintf("Deleted document ID: %s\n", objectID.Hex()))
		//}
	}

	log.Printf("Processed %d documents for deletion.", len(docs))
	return nil
}

// MongoDB에서 도큐먼트 삭제 여부 확인
func VerifyDeletion(ctx context.Context, collection *mongo.Collection, docs []map[string]interface{}) {
	for _, doc := range docs {
		rawID, ok := doc["_id"]
		if !ok {
			log.Printf("Document does not have an '_id' field: %v", doc)
			continue
		}

		var objectID primitive.ObjectID

		switch id := rawID.(type) {
		case primitive.ObjectID:
			objectID = id
		case string:
			parsedID, err := primitive.ObjectIDFromHex(id)
			if err != nil {
				log.Printf("Invalid ObjectID format for ID: %s, error: %v", id, err)
				continue
			}
			objectID = parsedID
		default:
			log.Printf("Unsupported '_id' type: %T for document: %v", id, doc)
			continue
		}

		filter := bson.M{"_id": objectID}
		count, err := collection.CountDocuments(ctx, filter)
		if err != nil {
			log.Printf("Error verifying deletion for ID %s: %v", objectID.Hex(), err)
			continue
		}
		if count > 0 {
			log.Printf("Document with ID %s still exists in the collection!", objectID.Hex())
		}
	}
}

// JSON 파일과 tar.gz 파일 삭제
func deleteLocalFiles(files []os.FileInfo, archiveName string) {
	// JSON 파일 삭제
	for _, file := range files {
		if filepath.Ext("/tmp/"+file.Name()) == ".json" {
			if err := os.Remove("/tmp/" + file.Name()); err != nil {
				log.Printf("Failed to delete local JSON file %s: %v", file.Name(), err)
			} else {
				log.Printf("Successfully deleted local JSON file: %s", file.Name())
			}
		}
	}

	// tar.gz 파일 삭제
	if err := os.Remove("/tmp/" + archiveName); err != nil {
		log.Printf("Failed to delete tar.gz file %s: %v", archiveName, err)
	} else {
		log.Printf("Successfully deleted tar.gz file: %s", archiveName)
	}
}
