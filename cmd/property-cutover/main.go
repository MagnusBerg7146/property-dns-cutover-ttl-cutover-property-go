package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"example.com/property-dns-cutover/internal/cutover"
)

type server struct {
	client *cutover.Client
}

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	s := &server{client: cutover.NewClient(key)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cutovers", s.cutOver)
	mux.HandleFunc("POST /rollbacks", s.rollback)
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	httpServer := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("property cutover service listening on %s", addr)
	log.Fatal(httpServer.ListenAndServe())
}

func (s *server) cutOver(w http.ResponseWriter, r *http.Request) {
	var input cutover.Request
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON request"})
		return
	}
	result, err := s.client.CutOver(r.Context(), input)
	if err != nil {
		writeClientError(w, err)
		return
	}
	status := http.StatusOK
	if result.Status == "pending" {
		status = http.StatusConflict
	}
	writeJSON(w, status, result)
}

type rollbackRequest struct {
	ZoneID   string         `json:"zone_id"`
	ChangeID string         `json:"change_id"`
	Previous cutover.Record `json:"previous"`
}

func (s *server) rollback(w http.ResponseWriter, r *http.Request) {
	var input rollbackRequest
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.ZoneID == "" || input.ChangeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "zone_id, change_id, and previous are required"})
		return
	}
	if err := s.client.Rollback(r.Context(), input.ZoneID, input.Previous, input.ChangeID); err != nil {
		writeClientError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rolled_back", "zone_id": input.ZoneID})
}

func writeClientError(w http.ResponseWriter, err error) {
	var apiErr *cutover.APIError
	if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
		writeJSON(w, apiErr.HTTPStatus, map[string]string{"error": apiErr.Code, "message": apiErr.Message})
		return
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": "upstream request failed"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
