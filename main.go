package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gustionusamba24/chirpy/internal/database"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type server struct {
	fileserverHits atomic.Int32
	database       *database.Queries
	platform       string
}

func writeJSONResponse(w http.ResponseWriter, response any) {
	encoder := json.NewEncoder(w)

	if err := encoder.Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *server) HandleFiles(prefix string) http.Handler {
	fileServer := http.StripPrefix(prefix, http.FileServer(http.Dir(".")))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.fileserverHits.Add(1)
		fileServer.ServeHTTP(w, r)
	})
}

func (s *server) HandleHealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (s *server) HandleReset(w http.ResponseWriter, r *http.Request) {
	if s.platform != "dev" {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	s.fileserverHits.Store(0)

	s.database.DeleteAllUsers(r.Context())

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (s *server) HandleMetrics(w http.ResponseWriter, r *http.Request) {
	currentHits := s.fileserverHits.Load()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	template := `
	<html>
		<body>
			<h1>Welcome, Chirpy Admin</h1>
			<p>Chirpy has been visited %d times!</p>
		</body>
	</html>
	`

	fmt.Fprintf(w, template, currentHits)
}

type chirpValidationRequest struct {
	Body string `json:"body"`
}

type chirpValidationResponse struct {
	CleanedBody string `json:"cleaned_body"`
}

type chirpValidationErrorResponse struct {
	Err string `json:"error"`
}

func (s *server) HandleChirpValidation(w http.ResponseWriter, r *http.Request) {
	blacklist := []string{
		"kerfuffle",
		"sharbert",
		"fornax",
	}

	var chirpReq chirpValidationRequest

	defer r.Body.Close()

	w.Header().Set("Content-Type", "application/json")

	err := json.NewDecoder(r.Body).Decode(&chirpReq)

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSONResponse(w, chirpValidationErrorResponse{
			Err: "Unable to deserialize JSON",
		})
		return
	}

	if len(chirpReq.Body) > 140 {
		w.WriteHeader(http.StatusBadRequest)
		writeJSONResponse(w, chirpValidationErrorResponse{
			Err: "Chirp is too long",
		})
		return
	}

	words := strings.Split(chirpReq.Body, " ")
	sanitized := []string{}

	for _, word := range words {
		if slices.Contains(blacklist, strings.ToLower(word)) {
			sanitized = append(sanitized, "****")
			continue
		}

		sanitized = append(sanitized, word)
	}

	cleanedBody := strings.Join(sanitized, " ")

	w.WriteHeader(http.StatusOK)
	writeJSONResponse(w, chirpValidationResponse{CleanedBody: cleanedBody})
}

type createUserRequest struct {
	Email string `json:"email"`
}

type createUserResponse struct {
	Id        string `json:"id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Email     string `json:"email"`
}

func (s *server) HandleCreateUser(w http.ResponseWriter, r *http.Request) {
	var createUserReq createUserRequest

	defer r.Body.Close()

	w.Header().Set("Content-Type", "application/json")

	err := json.NewDecoder(r.Body).Decode(&createUserReq)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSONResponse(w, chirpValidationErrorResponse{
			Err: "Unable to deserialize JSON",
		})
		return
	}

	trimmedEmail := strings.TrimSpace(createUserReq.Email)

	if trimmedEmail == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSONResponse(w, chirpValidationErrorResponse{
			Err: "Email is required. It must be valid email address",
		})
		return
	}

	user, err := s.database.CreateUser(r.Context(), trimmedEmail)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSONResponse(w, chirpValidationErrorResponse{
			Err: "Failed to create a user",
		})
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSONResponse(w, createUserResponse{
		Id:        user.ID.String(),
		CreatedAt: user.CreatedAt.Format(time.RFC3339),
		UpdatedAt: user.UpdatedAt.Format(time.RFC3339),
		Email:     user.Email,
	})
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Failed to load environment variable")
		os.Exit(1)
	}

	dbURL := os.Getenv("DB_URL")
	platform := os.Getenv("PLATFORM")

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	server := &server{
		database: database.New(db),
		platform: platform,
	}

	appRoot := "/app/"

	mux.Handle(appRoot, server.HandleFiles(appRoot))
	mux.HandleFunc("GET /admin/metrics", server.HandleMetrics)
	mux.HandleFunc("POST /admin/reset", server.HandleReset)

	mux.HandleFunc("GET /api/healthz", server.HandleHealthCheck)
	mux.HandleFunc("POST /api/validate_chirp", server.HandleChirpValidation)

	mux.HandleFunc("POST /api/users", server.HandleCreateUser)

	port := ":8080"

	httpServer := &http.Server{
		Addr:    port,
		Handler: mux,
	}

	go func() {
		log.Printf("Server started and listening on %s\n", port)

		// ListenAndServe always returns a non-nil error
		// after Shutdown or Close, the returned error is http.ErrServerClosed
		// program execution will stop at that line
		// the subsequent code will not be executed until the server is shutdown
		err := httpServer.ListenAndServe()

		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP Server error: %v", err)
		}

		log.Println("Stopped serving new connections")
	}()

	// main program will wait here until it receives a signal to shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	shutdownCtx, shutdownRelease := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownRelease()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("HTTP shutdown error: %v", err)
	}

	log.Println("Graceful shutdown completed")
}
