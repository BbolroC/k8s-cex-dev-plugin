# Start with a small base image
FROM golang:1.21-alpine as builder

# Set the working directory
WORKDIR /app

# Copy the Go module files
COPY go.mod go.sum ./

# Download the Go modules
RUN go mod download

# Copy the source code
COPY . .

# Build the Go binary
RUN go build -o webhook .

# Use a minimal base image to run the compiled binary
FROM alpine:latest

# Set the working directory
WORKDIR /root/

# Copy the binary from the builder stage
COPY --from=builder /app/webhook .

# Run the webhook server
CMD ["./webhook"]
