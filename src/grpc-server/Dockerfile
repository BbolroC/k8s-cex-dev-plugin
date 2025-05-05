# Dockerfile for zcrypt-grpc-server
FROM golang:1.23 AS builder

WORKDIR /app
COPY . .

WORKDIR /app/grpc-server
RUN go mod download
RUN go build -o grpc-server .

FROM ubuntu:latest
WORKDIR /root/

COPY --from=builder /app/grpc-server/grpc-server /root/

EXPOSE 50051

CMD ["/root/grpc-server"]
