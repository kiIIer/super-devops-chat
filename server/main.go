package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"

	"github.com/spf13/viper"
	"github.com/streadway/amqp"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	pb "super-devops-chat/proto"
)

// ChatMessage represents a chat message stored in the database
type ChatMessage struct {
	ID        uint `gorm:"primaryKey"`
	User      string
	Message   string
	Timestamp int64
}

// server represents the gRPC server
type server struct {
	pb.UnimplementedChatServiceServer
	db           *gorm.DB
	rabbitConn   *amqp.Connection
	saveExchange string
	newMessageEx string
}

// NewServer creates a new chat server instance
func NewServer(db *gorm.DB, rabbitConn *amqp.Connection, saveExchange, newMessageEx string) *server {
	return &server{
		db:           db,
		rabbitConn:   rabbitConn,
		saveExchange: saveExchange,
		newMessageEx: newMessageEx,
	}
}

// GetMessages retrieves past messages from the database
func (s *server) GetMessages(ctx context.Context, req *pb.Empty) (*pb.MessageList, error) {
	var messages []ChatMessage
	if err := s.db.Order("timestamp asc").Find(&messages).Error; err != nil {
		return nil, err
	}

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

// Chat handles bidirectional streaming for chat
func (s *server) Chat(stream pb.ChatService_ChatServer) error {
	ch, err := s.rabbitConn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	queue, err := ch.QueueDeclare(
		"",    // name
		false, // durable
		true,  // delete when unused
		true,  // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		return err
	}

	err = ch.QueueBind(
		queue.Name,     // queue name
		"",             // routing key
		s.newMessageEx, // exchange
		false,
		nil,
	)
	if err != nil {
		return err
	}

	msgs, err := ch.Consume(
		queue.Name, // queue
		"",         // consumer
		true,       // auto-ack
		false,      // exclusive
		false,      // no-local
		false,      // no-wait
		nil,        // args
	)
	if err != nil {
		return err
	}

	go func() {
		for msg := range msgs {
			var chatMsg pb.ChatMessage
			err := proto.Unmarshal(msg.Body, &chatMsg)
			if err != nil {
				log.Printf("Failed to unmarshal message: %v", err)
				continue
			}
			stream.Send(&chatMsg)
		}
	}()

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		body, err := proto.Marshal(msg)
		if err != nil {
			return err
		}

		err = ch.Publish(
			s.saveExchange,
			"",
			false,
			false,
			amqp.Publishing{
				ContentType: "application/protobuf",
				Body:        body,
			})
		if err != nil {
			return err
		}
	}
}

func main() {
	// Configuration setup
	viper.SetDefault("DB_HOST", "localhost")
	viper.SetDefault("DB_PORT", "5432")
	viper.SetDefault("DB_USER", "postgres")
	viper.SetDefault("DB_PASSWORD", "password")
	viper.SetDefault("DB_NAME", "chatdb")
	viper.SetDefault("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/")
	viper.SetDefault("SAVE_EXCHANGE", "SaveMessageExchange")
	viper.SetDefault("NEW_MESSAGE_EXCHANGE", "NewMessageExchange")

	viper.AutomaticEnv()

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable",
		viper.GetString("DB_HOST"),
		viper.GetString("DB_USER"),
		viper.GetString("DB_PASSWORD"),
		viper.GetString("DB_NAME"),
		viper.GetString("DB_PORT"))

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	db.AutoMigrate(&ChatMessage{})

	conn, err := amqp.Dial(viper.GetString("RABBITMQ_URL"))
	if err != nil {
		log.Fatalf("failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	saveExchange := viper.GetString("SAVE_EXCHANGE")
	newMessageEx := viper.GetString("NEW_MESSAGE_EXCHANGE")

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("failed to open a channel: %v", err)
	}
	defer ch.Close()

	err = ch.ExchangeDeclare(
		saveExchange, // name
		"direct",     // type
		true,         // durable
		false,        // auto-deleted
		false,        // internal
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		log.Fatalf("failed to declare an exchange: %v", err)
	}

	err = ch.ExchangeDeclare(
		newMessageEx, // name
		"fanout",     // type
		false,        // durable
		false,        // auto-deleted
		false,        // internal
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		log.Fatalf("failed to declare an exchange: %v", err)
	}

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	s := grpc.NewServer()
	chatServer := NewServer(db, conn, saveExchange, newMessageEx)
	pb.RegisterChatServiceServer(s, chatServer)
	log.Printf("server listening at %v", lis.Addr())
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
