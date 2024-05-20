package main

import (
	"context"
	"fmt"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"io"
	"log"
	"net"
	pb "super-devops-chat/proto"
	"sync"
)

type server struct {
	pb.UnimplementedChatServiceServer
	db      *gorm.DB
	mu      sync.Mutex
	clients map[pb.ChatService_ChatServer]struct{}
}

type ChatMessage struct {
	ID        uint `gorm:"primaryKey"`
	User      string
	Message   string
	Timestamp int64
}

func (s *server) Chat(stream pb.ChatService_ChatServer) error {
	s.mu.Lock()
	s.clients[stream] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, stream)
		s.mu.Unlock()
	}()

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		// Save the message to the database
		chatMsg := ChatMessage{
			User:      msg.User,
			Message:   msg.Message,
			Timestamp: msg.Timestamp,
		}
		s.db.Create(&chatMsg)

		// Broadcast the message to all clients
		s.mu.Lock()
		for client := range s.clients {
			client.Send(msg)
		}
		s.mu.Unlock()
	}
}

func (s *server) GetMessages(ctx context.Context, empty *pb.Empty) (*pb.MessageList, error) {
	var messages []ChatMessage
	s.db.Order("timestamp asc").Find(&messages)

	var pbMessages []*pb.ChatMessage
	for _, msg := range messages {
		pbMessages = append(pbMessages, &pb.ChatMessage{
			User:      msg.User,
			Message:   msg.Message,
			Timestamp: msg.Timestamp,
		})
	}

	return &pb.MessageList{Messages: pbMessages}, nil
}

func main() {
	viper.SetDefault("DB_HOST", "localhost")
	viper.SetDefault("DB_PORT", "5432")
	viper.SetDefault("DB_USER", "postgres")
	viper.SetDefault("DB_PASSWORD", "password")
	viper.SetDefault("DB_NAME", "chatdb")

	viper.AutomaticEnv()

	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		viper.GetString("DB_HOST"),
		viper.GetString("DB_PORT"),
		viper.GetString("DB_USER"),
		viper.GetString("DB_PASSWORD"),
		viper.GetString("DB_NAME"))

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	err = db.AutoMigrate(&ChatMessage{})
	if err != nil {
		log.Fatalf("failed to migrate database: %v", err)
		return
	}

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	srv := &server{
		db:      db,
		clients: make(map[pb.ChatService_ChatServer]struct{}),
	}
	pb.RegisterChatServiceServer(s, srv)
	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
