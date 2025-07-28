# gochive
<img width="1536" height="1024" alt="gochive" src="https://github.com/user-attachments/assets/78810b5c-dac9-4a6b-8285-9eba60c25152" />

```plaintext
go-chive는 MongoDB에서 아카이빙이 필요한 도큐먼트를 추출하여 S3 버킷에 보관하고 해당 도큐먼트를 삭제한다.
도큐먼트를 삭제하는 작업과 S3에 업로드 하는 작업은 주기(interval)가 맞물리는 경우 서로 뮤텍스로 보호된다.

(1) MongoDB 서버에서 아카이빙이 필요한 도큐먼트 fetch
→ 이 단계에서 오류가 발생하여도 도큐먼트를 조회만 했기 때문에 go-chive와 DB 서버 상에 아무런 문제 없음

(2) /tmp에 로컬 파일로 저장
→ 이 단계에서 오류가 발생하여도 도큐먼트를 조회만 했기 때문에 go-chive와 DB 서버 상에 문제 없음

(3) 로컬 파일을 모아서 압축 후, S3에 업로드
→ 이 단계에서 오류가 발생하여도 S3에 삭제 대상 도큐먼트가 업로드되고, 실제 도큐먼트는 삭제되지 않았기 때문에 go-chive와 DB 서버 상에 문제 없음. 중복 업로드된 도큐먼트는 추후 복구할 때 중복 체크하여 제거됨

(4) 아카이빙이 완료된 도큐먼트를 MongoDB에서 삭제
→ 이 단계에서 오류가 발생하거나 도큐먼트를 잘못 삭제했어도 S3에 업로드된 상태이기 때문에 삭제된 도큐먼트 복구 가능

(5). 도큐먼트 업로드 및 삭제가 완료되었기 때문에 로컬에 생성된 파일(bson, tar.gz) 제거
→ 이 단계에서 오류가 발생하면 /tmp 경로에 파일이 남게되나, 다음 삭제 작업에 영향은 없음
```

# How to build
```shell
$ go mod init go_chive
$ go mod tidy
$ go build .
```

# How to use
```shell
./go_chive -age-limit "1d" \
-archive-check-interval 60 \
-collection "test" \
-database "test" \
-host "mongodb+srv://<user>:<password>@test.db.prod.ilevit.com/?tls=false" \
-s3-bucket-uri "levit-database-archive-prod/test/" \
-s3-upload-interval 80 \
-archive-field "createdAt" \
-query-limit 1000
```

# Push to AWS ECR
```shell
# ▼ 아래 중 택일
###########################
# x86 아키텍처에서만 빌드할 경우
###########################
# 도커 이미지 빌드
$ docker build --platform linux/amd64 --tag <AWS ECR URI>:1.0.0 . --file ./Dockerfile

# AWS ECR 로그인
$ aws ecr get-login-password | docker login --username AWS --password-stdin <Account ID>.dkr.ecr.ap-northeast-2.amazonaws.com

# 도커 푸시
$ docker push <AWS ECR URI>:1.0.0

##############################################
# x86 + arm64 아키텍처에서 멀티 플랫폼으로 빌드할 경우
##############################################
# 이 경우 빌드와 함께 ECR 푸시까지 진행된다.
$ docker buildx build \
  --platform linux/amd64,linux/arm64 \
  --tag <AWS ECR URI>:1.0.0 \
  --push \
  . --file ./Dockerfile
```

# How to add new collection
`devops-gitops-manifest > clusters > alwayz-prod-eks > workloads > db-go-chive > values.yaml`
values.yaml 파일에 아래와 같이 신규 컬렉션 정보를 등록하고 배포하면 완료
```shell
# archiveField에는 반드시 인덱스가 생성되어 있어야 하며, 생성이 누락되면 COLSCAN 수행되어 DB에 부하가 발생합니다.
  - name: incubator-offerwall-event-log
    hostAddress: "incubator"
    ageLimit: 92d
    queryLimit: 2000
    archiveCheckInterval: 9
    collection: offerwall_event_log
    database: ALWAYZ
    archiveField: createdAt
    s3:
      bucket: levit-database-archive-prod/incubator/offerwall_event_log/
      uploadInterval: 65
```
- `name`: go-chive 파드명
- `hostAddress`: MongoDB 클러스터명
    - ex) prod-incubator-shard01이면 incubator만 기입
- `ageLimit`: 몇 일이 지난 도큐먼트를 삭제할 것인지 설정
    - ex) 92d라면 92일이 지난 도큐먼트가 삭제 대상
- `queryLimit`: Request 한 번에 처리할 도큐먼트 수. 조회 배치 단위
- `archiveCheckInterval`: Request 요청당 간격(seconds)
    - ex) 9로 설정할 경우 9초마다 ageLimit 만큼의 도큐먼트 검색
- `collection`: 삭제 대상 컬렉션
- `database`: 삭제 대상 컬렉션의 논리 데이터베이스명
- `archiveField`: 삭제 조건 필드
    - ex) createdAt이면 생성 날짜 기준으로 ageLimit 이전에 생성된 도큐먼트들이 삭제됨
- `s3.bucket`: 삭제 대상 도큐먼트가 아카이빙될 S3 버킷 경로
- `s3.uploadInterval`: 검색한 도큐먼트가 저장된 파일들을 몇 초마다 압축해서 S3에 업로드할 것인지 설정
    - ex) 65라면 65초마다 S3에 업로드
