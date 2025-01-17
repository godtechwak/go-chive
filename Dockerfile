FROM golang:1.23 AS builder

WORKDIR /app

COPY go.mod go.sum ./

COPY . .

RUN env GOARCH=amd64 GOOS=linux CGO_ENABLED=0 go build -o /go-chive .

FROM amazonlinux:latest

COPY --from=builder /go-chive /go-chive
USER nobody
ENTRYPOINT ["/go-chive"]
