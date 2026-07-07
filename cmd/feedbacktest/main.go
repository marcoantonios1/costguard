package main

import (
	"log"
	"net/http"

	"github.com/marcoantonios1/costguard/internal/feedback"
	"github.com/marcoantonios1/costguard/internal/server"
)

func main() {
	store, err := feedback.NewJSONLStore("/tmp/feedback_stats.jsonl")
	if err != nil {
		log.Fatal(err)
	}
	h := feedback.NewHandler(store, nil)
	reader := feedback.NewReader("/tmp/feedback_stats.jsonl")
	statsHandler := feedback.NewStatsHandler(reader)

	mux := http.NewServeMux()
	mux.Handle("/v1/feedback", server.AdminAuth("dev-key")(h))
	mux.Handle("/v1/feedback/stats", server.AdminAuth("dev-key")(statsHandler))

	log.Println("listening on :8091")
	log.Fatal(http.ListenAndServe(":8091", mux))
}
