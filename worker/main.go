package main

import (
	"fmt"
	"log"

	"github.com/spf13/viper"
	"github.com/streadway/amqp"
	"google.golang.org/protobuf/proto"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	pb "super-devops-chat/proto"
)

type ChatMessage struct {
	ID        uint `gorm:"primaryKey"`
	User      string
	Message   string
	Timestamp int64
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

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("failed to open a channel: %v", err)
	}
	defer ch.Close()

	saveExchange := viper.GetString("SAVE_EXCHANGE")
	newMessageEx := viper.GetString("NEW_MESSAGE_EXCHANGE")

	queue, err := ch.QueueDeclare(
		"",    // name
		true,  // durable
		true,  // delete when unused
		false, // exclusive
		false, // no-wait
		nil,   // arguments
	)
	if err != nil {
		log.Fatalf("failed to declare a queue: %v", err)
	}

	err = ch.QueueBind(
		queue.Name,   // queue name
		"",           // routing key
		saveExchange, // exchange
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("failed to bind a queue: %v", err)
	}

	msgs, err := ch.Consume(
		queue.Name, // queue
		"",         // consumer
		false,      // auto-ack
		false,      // exclusive
		false,      // no-local
		false,      // no-wait
		nil,        // args
	)
	if err != nil {
		log.Fatalf("failed to register a consumer: %v", err)
	}

	for msg := range msgs {
		var chatMsg pb.ChatMessage
		err := proto.Unmarshal(msg.Body, &chatMsg)
		if err != nil {
			log.Printf("Failed to unmarshal message: %v", err)
			msg.Nack(false, true)
			continue
		}

		// Save to database
		dbMsg := ChatMessage{
			User:      chatMsg.User,
			Message:   chatMsg.Message,
			Timestamp: chatMsg.Timestamp,
		}
		db.Create(&dbMsg)

		// Publish to NewMessageExchange
		body, err := proto.Marshal(&chatMsg)
		if err != nil {
			log.Printf("Failed to marshal message: %v", err)
			msg.Nack(false, true)
			continue
		}

		err = ch.Publish(
			newMessageEx,
			"",
			false,
			false,
			amqp.Publishing{
				ContentType: "application/protobuf",
				Body:        body,
			})
		if err != nil {
			log.Printf("Failed to publish message: %v", err)
			msg.Nack(false, true)
			continue
		}

		msg.Ack(false)
	}
}
