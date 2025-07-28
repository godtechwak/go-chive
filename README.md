# go-chive
MongoDB에서 아카이빙이 필요한 도큐먼트를 추출하여 S3 버킷에 보관 및 삭제하는 도구
<img width="1536" height="1024" alt="gochive" src="https://github.com/user-attachments/assets/0618bf4c-9ed3-4c86-94f4-eb9b28ac673c" />


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
-host "mongodb+srv://<user>:<password>@test.db.bumhwak.com/?tls=false" \
-s3-bucket-uri "levit-database-archive-prod/test/" \
-s3-upload-interval 80 \
-archive-field "createdAt" \
-query-limit 100
```

# Push to AWS ECR
```shell
docker build --platform linux/amd64 --tag <AWS ECR URI>:1.0.0-go-chive . --file ./Dockerfile

aws ecr get-login-password | docker login --username AWS --password-stdin <Account ID>.dkr.ecr.ap-northeast-2.amazonaws.com

docker push <AWS ECR URI>:1.0.0-go-chive
```
