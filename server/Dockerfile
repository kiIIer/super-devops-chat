# Use the official Golang image as a build stage
FROM golang:1.22 as builder

# Set the working directory inside the container
WORKDIR /app

# Copy the go.mod and go.sum files to download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the application code
COPY . .

# Ensure all dependencies are downloaded and go.sum is updated
RUN go mod tidy

# Build the Go app
WORKDIR /app/server
RUN go build -o server .

# Use the official Golang image as the runtime stage
FROM golang:1.22

# Set the working directory inside the container
WORKDIR /app

# Copy the built binary from the build stage
COPY --from=builder /app/server/server /app/server

# Copy the proto files (optional, if needed by the server at runtime)
COPY --from=builder /app/proto /app/proto

# Ensure the binary has execution permissions
RUN chmod +x /app/server

# Command to run the server
CMD ["/app/server"]
