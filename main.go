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
	jwtSecret      string
}

func encodeJsonResponse(w http.ResponseWriter, statusCode int, response any) {
	w.WriteHeader(statusCode)
	encoder := json.NewEncoder(w)

	if err := encoder.Encode(response); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func decodeJsonRequest[T any](w http.ResponseWriter, r *http.Request) (*T, error) {
	var data *T
	w.Header().Set("Content-Type", "application/json")
	decoder := json.NewDecoder(r.Body)
	defer r.Body.Close()
	err := decoder.Decode(&data)

	if err != nil {
		return nil, apiErrorResponse{
			Err: "Unable to deserialize JSON",
		}
	}

	return data, err
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

func (e apiErrorResponse) Error() string {
	return e.Err
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

	createChirpReq, err := decodeJsonRequest[createChirpRequest](w, r)
	if err != nil {
		encodeJsonResponse(w, http.StatusBadRequest, err)
		return
	}

	if len(createChirpReq.Body) > 140 {
		encodeJsonResponse(w, http.StatusBadRequest, apiErrorResponse{
			Err: "Chirp is too long",
		})
		return
	}

	tokenString, err := auth.GetBearerToken(r.Header)
	if err != nil {
		encodeJsonResponse(w, http.StatusUnauthorized, apiErrorResponse{
			Err: "Unauthorized",
		})
		return
	}

	requestUserId, err := auth.ValidateJWT(tokenString, s.jwtSecret)
	if err != nil {
		encodeJsonResponse(w, http.StatusUnauthorized, apiErrorResponse{
			Err: "Unauthorized",
		})
		return
	}

	existingUser, err := s.database.GetUserById(r.Context(), requestUserId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			encodeJsonResponse(w, http.StatusNotFound, apiErrorResponse{
				Err: "User not found",
			})
			return
		}

		encodeJsonResponse(w, http.StatusInternalServerError, apiErrorResponse{
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
		encodeJsonResponse(w, http.StatusInternalServerError, apiErrorResponse{
			Err: "Failed to create a chirp",
		})
		return
	}

	encodeJsonResponse(w, http.StatusCreated, chirpResponse{
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
	createUserReq, err := decodeJsonRequest[createUserRequest](w, r)
	if err != nil {
		encodeJsonResponse(w, http.StatusBadRequest, err)
		return
	}

	trimmedEmail := strings.TrimSpace(createUserReq.Email)

	if trimmedEmail == "" {
		encodeJsonResponse(w, http.StatusBadRequest, apiErrorResponse{
			Err: "Email is required. It must be valid email address",
		})
		return
	}

	trimmedPassword := strings.TrimSpace(createUserReq.Password)

	if trimmedPassword == "" {
		encodeJsonResponse(w, http.StatusBadRequest, apiErrorResponse{
			Err: "Password is required. It must be a non-empty password",
		})
		return
	}

	hashedPassword, err := auth.HashPassword(trimmedPassword)
	if err != nil {
		encodeJsonResponse(w, http.StatusInternalServerError, apiErrorResponse{
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
		encodeJsonResponse(w, http.StatusInternalServerError, apiErrorResponse{
			Err: "Failed to create a user",
		})
		return
	}

	encodeJsonResponse(w, http.StatusCreated, createUserResponse{
		Id:        user.ID.String(),
		CreatedAt: user.CreatedAt.Format(time.RFC3339),
		UpdatedAt: user.UpdatedAt.Format(time.RFC3339),
		Email:     user.Email,
	})
}

func (s *server) HandleGetAllChirps(w http.ResponseWriter, r *http.Request) {
	chirps, err := s.database.GetAllChirps(r.Context())
	if err != nil {
		encodeJsonResponse(w, http.StatusInternalServerError, apiErrorResponse{
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

	encodeJsonResponse(w, http.StatusOK, chirpsResponse)
}

func (s *server) HandleGetChirp(w http.ResponseWriter, r *http.Request) {
	chirpId := r.PathValue("id")

	validChirdId, err := uuid.Parse(chirpId)
	if err != nil {
		encodeJsonResponse(w, http.StatusBadRequest, apiErrorResponse{
			Err: "Invalid chirp ID. Chirp ID must be valid UUID",
		})
		return
	}

	chirp, err := s.database.GetChirpByID(r.Context(), validChirdId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			encodeJsonResponse(w, http.StatusNotFound, apiErrorResponse{
				Err: "No chirp found with the provided ID",
			})
			return
		}

		encodeJsonResponse(w, http.StatusInternalServerError, apiErrorResponse{
			Err: "Failed to retrieve chirp",
		})
		return
	}

	encodeJsonResponse(w, http.StatusOK, chirpResponse{
		Id:        chirp.ID.String(),
		CreatedAt: chirp.CreatedAt.Format(time.RFC3339),
		UpdatedAt: chirp.UpdatedAt.Format(time.RFC3339),
		Body:      chirp.Body,
		UserId:    chirp.UserID.String(),
	})
}

type loginRequest struct {
	Email            string `json:"email"`
	Password         string `json:"password"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
}

type loginResponse struct {
	Id        string `json:"id"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	Email     string `json:"email"`
	Token     string `json:"token"`
}

func (r *loginRequest) Validate() error {
	if strings.TrimSpace(r.Email) != "" && strings.TrimSpace(r.Password) != "" {
		return nil
	}

	return apiErrorResponse{
		Err: "Email or password is required. Please provide both email and password",
	}
}

func (s *server) HandleLogin(w http.ResponseWriter, r *http.Request) {
	loginReq, err := decodeJsonRequest[loginRequest](w, r)
	if err != nil {
		encodeJsonResponse(w, http.StatusBadRequest, err)
		return
	}

	loginReq.Email = strings.TrimSpace(loginReq.Email)
	loginReq.Password = strings.TrimSpace(loginReq.Password)

	if err := loginReq.Validate(); err != nil {
		encodeJsonResponse(w, http.StatusBadRequest, err)
		return
	}

	existingUser, err := s.database.GetUserByEmail(r.Context(), loginReq.Email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			encodeJsonResponse(w, http.StatusUnauthorized, apiErrorResponse{
				Err: "Invalid email or password",
			})
			return
		}

		encodeJsonResponse(w, http.StatusInternalServerError, apiErrorResponse{
			Err: "Failed to retrieve user",
		})
		return
	}

	match, err := auth.CheckPasswordHash(loginReq.Password, existingUser.HashedPassword)
	if err != nil {
		encodeJsonResponse(w, http.StatusInternalServerError, apiErrorResponse{
			Err: "Something went wrong",
		})
		return
	}

	if !match {
		encodeJsonResponse(w, http.StatusUnauthorized, apiErrorResponse{
			Err: "Incorrect email or password",
		})
		return
	}

	expiresIn := time.Hour
	if loginReq.ExpiresInSeconds > 0 {
		expiresIn = time.Duration(loginReq.ExpiresInSeconds) * time.Second
		if expiresIn > time.Hour {
			expiresIn = time.Hour
		}
	}

	token, err := auth.MakeJWT(existingUser.ID, s.jwtSecret, expiresIn)
	if err != nil {
		encodeJsonResponse(w, http.StatusInternalServerError, apiErrorResponse{
			Err: "Failed to create token",
		})
		return
	}

	encodeJsonResponse(w, http.StatusOK, loginResponse{
		Id:        existingUser.ID.String(),
		CreatedAt: existingUser.CreatedAt.Format(time.RFC3339),
		UpdatedAt: existingUser.UpdatedAt.Format(time.RFC3339),
		Email:     existingUser.Email,
		Token:     token,
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
	jwtSecret := os.Getenv("JWT_SECRET_KEY")
	if jwtSecret == "" {
		log.Fatalf("JWT_SECRET_KEY is not set")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	server := &server{
		database:  database.New(db),
		platform:  platform,
		jwtSecret: jwtSecret,
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
