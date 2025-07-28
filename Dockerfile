FROM golang:1.23 AS builder

WORKDIR /app

COPY go.mod go.sum ./

COPY . .

#RUN env GOARCH=amd64 GOOS=linux CGO_ENABLED=0 go build -o /go-chive .

RUN env GOOS=linux CGO_ENABLED=0 go build -o /go-chive .
FROM amazonlinux:latest

#ADD https://busybox.net/downloads/binaries/1.31.0-defconfig-multiarch-musl/busybox-x86_64 /bin/busybox
#RUN chmod +x /bin/busybox

#RUN /bin/busybox --install

COPY --from=builder /go-chive /go-chive
USER nobody
ENTRYPOINT ["/go-chive"]
