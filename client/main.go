package main

import (
	"bufio"
	"context"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"
	"log"
	"os"
	pb "super-devops-chat/proto" // Updated import path
	"time"
)

func main() {
	// Set up a connection to the server with context and proper options.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := grpc.NewClient("localhost:50051", grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithConnectParams(grpc.ConnectParams{Backoff: backoff.DefaultConfig, MinConnectTimeout: 10 * time.Second}))
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	defer func(conn *grpc.ClientConn) {
		err := conn.Close()
		if err != nil {
			return
		}
	}(conn)

	c := pb.NewChatServiceClient(conn)

	// Get and print the messages
	getMessages(ctx, c)

	// Start the chat
	startChat(ctx, c)
}

func getMessages(ctx context.Context, c pb.ChatServiceClient) {
	resp, err := c.GetMessages(ctx, &pb.Empty{})
	if err != nil {
		log.Fatalf("could not get messages: %v", err)
	}

	fmt.Println("Previous messages:")
	for _, msg := range resp.Messages {
		fmt.Printf("%s: %s\n", msg.User, msg.Message)
	}
	fmt.Println()
}

func startChat(ctx context.Context, c pb.ChatServiceClient) {
	stream, err := c.Chat(context.Background())
	if err != nil {
		log.Fatalf("could not connect: %v", err)
	}

	// Goroutine to receive messages from the server
	go func() {
		for {
			in, err := stream.Recv()
			if err != nil {
				log.Fatalf("Failed to receive a message: %v", err)
			}
			fmt.Printf("%s: %s\n", in.User, in.Message)
		}
	}()

	// Reading user input and sending messages to the server
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Enter your name:")
	user, _ := reader.ReadString('\n')
	user = user[:len(user)-1] // Remove newline character

	for {
		fmt.Print("Enter message: ")
		text, _ := reader.ReadString('\n')
		text = text[:len(text)-1] // Remove newline character
		if err := stream.Send(&pb.ChatMessage{User: user, Message: text, Timestamp: time.Now().Unix()}); err != nil {
			log.Fatalf("Failed to send a message: %v", err)
		}
	}
}
