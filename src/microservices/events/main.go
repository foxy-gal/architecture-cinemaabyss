package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
)

type config struct {
	port    string
	brokers []string
}

type Event struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Timestamp string                 `json:"timestamp"`
	Payload   map[string]interface{} `json:"payload"`
}

type EventResponse struct {
	Status    string `json:"status"`
	Partition int    `json:"partition"`
	Offset    int64  `json:"offset"`
	Event     Event  `json:"event"`
}

func main() {
	cfg := loadConfig()

	writers := map[string]*kafka.Writer{
		"movie-events":   newWriter(cfg.brokers, "movie-events"),
		"user-events":    newWriter(cfg.brokers, "user-events"),
		"payment-events": newWriter(cfg.brokers, "payment-events"),
	}
	for _, writer := range writers {
		defer writer.Close()
	}

	startConsumer(cfg.brokers, "movie-events")
	startConsumer(cfg.brokers, "user-events")
	startConsumer(cfg.brokers, "payment-events")

	mux := http.NewServeMux()
	mux.HandleFunc("/api/events/health", healthHandler)
	mux.HandleFunc("/api/events/movie", makeEventHandler("movie", writers["movie-events"]))
	mux.HandleFunc("/api/events/user", makeEventHandler("user", writers["user-events"]))
	mux.HandleFunc("/api/events/payment", makeEventHandler("payment", writers["payment-events"]))

	log.Printf("Events service listening on port %s", cfg.port)
	log.Fatal(http.ListenAndServe(":"+cfg.port, mux))
}

func loadConfig() config {
	port := os.Getenv("PORT")
	brokersRaw := os.Getenv("KAFKA_BROKERS")
	brokers := []string{}
	if brokersRaw != "" {
		brokers = strings.Split(brokersRaw, ",")
	}

	return config{
		port:    port,
		brokers: brokers,
	}
}

func newWriter(brokers []string, topic string) *kafka.Writer {
	return &kafka.Writer{
		Addr:     kafka.TCP(brokers...),
		Topic:    topic,
		Balancer: &kafka.LeastBytes{},
	}
}

func startConsumer(brokers []string, topic string) {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: brokers,
		GroupID: "events-service",
		Topic:   topic,
	})

	go func() {
		defer reader.Close()
		for {
			msg, err := reader.ReadMessage(context.Background())
			if err != nil {
				log.Printf("consumer error for %s: %v", topic, err)
				continue
			}
			log.Printf("consumed %s: %s", topic, string(msg.Value))
		}
	}()
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"status": true})
}

func makeEventHandler(eventType string, writer *kafka.Writer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		payload := map[string]interface{}{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		event := Event{
			ID:        fmt.Sprintf("%s-%d", eventType, time.Now().UnixNano()),
			Type:      eventType,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Payload:   payload,
		}

		messageBytes, err := json.Marshal(event)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := writer.WriteMessages(context.Background(), kafka.Message{Value: messageBytes}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		response := EventResponse{
			Status:    "success",
			Partition: 0,
			Offset:    0,
			Event:     event,
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(response)
	}
}
