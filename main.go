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

	"github.com/google/uuid"
	"github.com/gustionusamba24/chirpy/internal/auth"
	"github.com/gustionusamba24/chirpy/internal/database"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

type server struct {
	fileserverHits atomic.Int32
	database       *database.Queries
	platform       string
}

func encodeJsonResponse(w http.ResponseWriter, response any) {
	encoder := json.NewEncoder(w)

	if err := encoder.Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func decodeJsonRequest[T any](w http.ResponseWriter, r *http.Request) *T {
	var data *T

	decoder := json.NewDecoder(r.Body)

	defer r.Body.Close()

	if err := decoder.Decode(&data); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		encodeJsonResponse(w, apiErrorResponse{
			Err: "Unable to deserialize JSON",
		})
		return nil
	}

	return data
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

type apiErrorResponse struct {
	Err string `json:"error"`
}

type createChirpRequest struct {
	Body   string `json:"body"`
	UserId string `json:"user_id"`
}

type chirpResponse struct {
	Id        string `json:"id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Body      string `json:"body"`
	UserId    string `json:"user_id"`
}

func (s *server) HandleCreateChirp(w http.ResponseWriter, r *http.Request) {
	blacklist := []string{
		"kerfuffle",
		"sharbert",
		"fornax",
	}

	w.Header().Set("Content-Type", "application/json")

	createChirpReq := decodeJsonRequest[createChirpRequest](w, r)

	if len(createChirpReq.Body) > 140 {
		w.WriteHeader(http.StatusBadRequest)
		encodeJsonResponse(w, apiErrorResponse{
			Err: "Chirp is too long",
		})
		return
	}

	requestUserId, err := uuid.Parse(createChirpReq.UserId)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		encodeJsonResponse(w, apiErrorResponse{
			Err: "Invalid user ID. User ID must be valid UUID",
		})
		return
	}

	existingUser, err := s.database.GetUserById(r.Context(), requestUserId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			encodeJsonResponse(w, apiErrorResponse{
				Err: "User not found",
			})
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		encodeJsonResponse(w, apiErrorResponse{
			Err: "Failed to retrieve user",
		})
		return
	}

	words := strings.Split(createChirpReq.Body, " ")
	sanitized := []string{}

	for _, word := range words {
		if slices.Contains(blacklist, strings.ToLower(word)) {
			sanitized = append(sanitized, "****")
			continue
		}

		sanitized = append(sanitized, word)
	}

	cleanedBody := strings.Join(sanitized, " ")

	createChirpParams := database.CreateChirpParams{
		Body:   cleanedBody,
		UserID: existingUser.ID,
	}

	chirp, err := s.database.CreateChirp(r.Context(), createChirpParams)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		encodeJsonResponse(w, apiErrorResponse{
			Err: "Failed to create a chirp",
		})
		return
	}

	w.WriteHeader(http.StatusCreated)
	encodeJsonResponse(w, chirpResponse{
		Id:        chirp.ID.String(),
		CreatedAt: chirp.CreatedAt.Format(time.RFC3339),
		UpdatedAt: chirp.UpdatedAt.Format(time.RFC3339),
		Body:      chirp.Body,
		UserId:    chirp.UserID.String(),
	})
}

type createUserRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type createUserResponse struct {
	Id        string `json:"id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Email     string `json:"email"`
}

func (s *server) HandleCreateUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	createUserReq := decodeJsonRequest[createUserRequest](w, r)

	trimmedEmail := strings.TrimSpace(createUserReq.Email)

	if trimmedEmail == "" {
		w.WriteHeader(http.StatusBadRequest)
		encodeJsonResponse(w, apiErrorResponse{
			Err: "Email is required. It must be valid email address",
		})
		return
	}

	trimmedPassword := strings.TrimSpace(createUserReq.Password)

	if trimmedPassword == "" {
		w.WriteHeader(http.StatusBadRequest)
		encodeJsonResponse(w, apiErrorResponse{
			Err: "Password is required. It must be a non-empty password",
		})
		return
	}

	hashedPassword, err := auth.HashPassword(trimmedPassword)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		encodeJsonResponse(w, apiErrorResponse{
			Err: "Failed to hash password",
		})
		return
	}

	createUserParams := database.CreateUserParams{
		Email:          trimmedEmail,
		HashedPassword: hashedPassword,
	}

	user, err := s.database.CreateUser(r.Context(), createUserParams)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		encodeJsonResponse(w, apiErrorResponse{
			Err: "Failed to create a user",
		})
		return
	}

	w.WriteHeader(http.StatusCreated)
	encodeJsonResponse(w, createUserResponse{
		Id:        user.ID.String(),
		CreatedAt: user.CreatedAt.Format(time.RFC3339),
		UpdatedAt: user.UpdatedAt.Format(time.RFC3339),
		Email:     user.Email,
	})
}

func (s *server) HandleGetAllChirps(w http.ResponseWriter, r *http.Request) {
	chirps, err := s.database.GetAllChirps(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		encodeJsonResponse(w, apiErrorResponse{
			Err: "Failed to get all chirps",
		})
		return
	}

	chirpsResponse := []chirpResponse{}

	for _, chirp := range chirps {
		chirpsResponse = append(chirpsResponse, chirpResponse{
			Id:        chirp.ID.String(),
			CreatedAt: chirp.CreatedAt.Format(time.RFC3339),
			UpdatedAt: chirp.UpdatedAt.Format(time.RFC3339),
			Body:      chirp.Body,
			UserId:    chirp.UserID.String(),
		})
	}

	w.WriteHeader(http.StatusOK)
	encodeJsonResponse(w, chirpsResponse)
}

func (s *server) HandleGetChirp(w http.ResponseWriter, r *http.Request) {
	chirpId := r.PathValue("id")

	validChirdId, err := uuid.Parse(chirpId)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		encodeJsonResponse(w, apiErrorResponse{
			Err: "Invalid chirp ID. Chirp ID must be valid UUID",
		})
		return
	}

	chirp, err := s.database.GetChirpByID(r.Context(), validChirdId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNotFound)
			encodeJsonResponse(w, apiErrorResponse{
				Err: "No chirp found with the provided ID",
			})
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		encodeJsonResponse(w, apiErrorResponse{
			Err: "Failed to retrieve chirp",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	encodeJsonResponse(w, chirpResponse{
		Id:        chirp.ID.String(),
		CreatedAt: chirp.CreatedAt.Format(time.RFC3339),
		UpdatedAt: chirp.UpdatedAt.Format(time.RFC3339),
		Body:      chirp.Body,
		UserId:    chirp.UserID.String(),
	})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResponse struct {
	Id        string `json:"id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Email     string `json:"email"`
}

func (s *server) HandleLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	loginReq := decodeJsonRequest[loginRequest](w, r)

	existingUser, err := s.database.GetUserByEmail(r.Context(), loginReq.Email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusUnauthorized)
			encodeJsonResponse(w, apiErrorResponse{
				Err: "Invalid email or password",
			})
			return
		}

		w.WriteHeader(http.StatusInternalServerError)
		encodeJsonResponse(w, apiErrorResponse{
			Err: "Failed to retrieve user",
		})
		return
	}

	match, err := auth.CheckPasswordHash(loginReq.Password, existingUser.HashedPassword)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		encodeJsonResponse(w, apiErrorResponse{
			Err: "Failed to check password",
		})
		return
	}

	if !match {
		w.WriteHeader(http.StatusUnauthorized)
		encodeJsonResponse(w, apiErrorResponse{
			Err: "Incorrect email or password",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	encodeJsonResponse(w, loginResponse{
		Id:        existingUser.ID.String(),
		CreatedAt: existingUser.CreatedAt.Format(time.RFC3339),
		UpdatedAt: existingUser.UpdatedAt.Format(time.RFC3339),
		Email:     existingUser.Email,
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

	mux.HandleFunc("POST /api/login", server.HandleLogin)
	mux.HandleFunc("POST /api/users", server.HandleCreateUser)

	mux.HandleFunc("GET /api/chirps", server.HandleGetAllChirps)
	mux.HandleFunc("GET /api/chirps/{id}", server.HandleGetChirp)
	mux.HandleFunc("POST /api/chirps", server.HandleCreateChirp)

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
