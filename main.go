package main

import (
	"embed"         // Allows embedding files into binary at compile time
	"html/template" // HTML templating engine for rendering dynamic web pages
	"io/fs"         // Gives FS utilities; fs.Sub() lets us serve from the "web/static" subdirectory
	"log"           // For Logging errors and info messages
	"net/http"      // For HTTP server and client funcionality
	"os"            // For OS interface

	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/mux" // Router for advanced URL Routing

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

/*
Embeds help us bake our files into our binary code so that we don't have to deploy with out files separately.
In our case, we bake the UI files into our binary, but it embed allows us to do it with any file.

Comments above the embed var are for the directory location and are FUNCTIONAL comments, so don't remove;
'go:embed' is a compiler directives
*/

//go:embed web/templates/*.html
var templatesFS embed.FS // FS stands for file-system

//go:embed web/static/*
var staticFS embed.FS

// This context type variable can carry things like cancellation signals,
// deadlines, and values across API boundaries and goroutines.
var ctx = context.Background()

type RoundMode string

const (
	ModeSample       RoundMode = "sample"
	ModeCollabRandom RoundMode = "collab-random"
	ModeCollabCyclic RoundMode = "collab-cyclic"
	ModeCollabPair   RoundMode = "collab-pair"
	ModeTelephone    RoundMode = "telephone"
)

type RoundState string

const (
	StateWaiting RoundState = "waiting"
	StateActive  RoundState = "active"
	StateClosed  RoundState = "closed"
)

type Participant struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	IsHost      bool      `json:"isHost"`
	JoinedAt    time.Time `json:"joinedAt"`
}

type Submission struct {
	ParticipantID string    `json:"participantId"`
	Filename      string    `json:"filename"`
	OriginalName  string    `json:"originalName"`
	UploadedAt    time.Time `json:"uploadedAt"`
	AssignedToID  string    `json:"assignedToId,omitempty"`
}

type Round struct {
	ID                 string                  `json:"id"`
	Name               string                  `json:"name"`
	Mode               RoundMode               `json:"mode"`
	JoinCode           string                  `json:"joinCode"`
	State              RoundState              `json:"state"`
	HostID             string                  `json:"hostId"`
	Participants       map[string]*Participant `json:"participants"`
	Submissions        map[string]*Submission  `json:"submissions"`
	AllowGuestDownload bool                    `json:"allowGuestDownload"`
	CreatedAt          time.Time               `json:"createdAt"`
	SampleFileID       string                  `json:"sampleFileId,omitempty"` // Particularly for sample mode
}

type Session struct {
	Token         string    `json:"token"`
	ParticipantID string    `json:"participantId"`
	RoundCode     string    `json:"roundCode"`
	CreatedAt     time.Time `json:"createdAt"`
}

type Server struct {
	db        *redis.Client      // Pointer to database connection
	templates *template.Template // parsed HTML templates
	router    *mux.Router        //HTTP router for handling different URLs
}

func main() {
	/*
		os.MkdirAll makes sure data/uploads exists (if not, it creates), and gives permission 0755
		for the 0000 format: (1st 0: special bit [we can ignore rn], 2nd 0: owner (you),
								3rd 0: group [users in your same group], 4th 0: others [everyone else])

		Octoal digit explanations:
		4: read only
		6: read + write
		7: read + write + execute
		5: read + execute

		0755 means that owner has RWX (read, write, and exectute), group has RX, and others have RX
	*/

	if err := os.MkdirAll("temp/uploads", 0755); err != nil {
		log.Fatal("Failed to create data directories:", err)
	}

	rdb := initRDB()  // Initialize database
	defer rdb.Close() // "defer" ensures db.close() runs when main() exits (cleanup)

	// ParseFS reads from the embedded FS that we created earlier here; We parse all embeddded HTML templates into memory
	templates, err := template.ParseFS(templatesFS, "web/templates/*.html")
	if err != nil {
		log.Fatal("Failed to parse from the embedded FS")
	}

	// Initializing new server (we ofc want a pointer because all those member vars are shared resources; Not
	// good to be copying large structs around either and also wouldn't make sense to)
	server := &Server{
		db:        rdb,
		templates: templates,
		router:    mux.NewRouter(),
	}

	// This uses the function below to register URL paths and link them to their handler functions
	server.setupRoutes()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("Server starting on http://localhost:%s", port)
	if err := http.ListenAndServe(":"+port, server.router); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}

/*
This is a member function for the Server class; 's' is the equivalent of "self" in python.

setupRoutes() defines how incoming URLs map to specific handler functions (e.g., "/", "/upload").
This ensures all routes are registered before the server starts listening.
*/
func (s *Server) setupRoutes() {
	// Static Files

	/*
		Let's define handler first: A handler is a thing (function or object) that takes an incoming HTTP request
		and writes back a response.

		fs.Sub(staticFS, "web/static") re-roots the embedded filesystem so that lookups like "css/main.css"
		resolve correctly inside the embedded "web/static" folder.

		PathPrefix("/static/") from Gorilla Mux matches any URL that begins with "/static/", letting us mount
		everything under that path.

		http.StripPrefix("/static/", ...) removes /static/ from the incoming URL path before passing is to the file server.
		For example, request "/static/css/app.css" -> file server sees "css/app.css"

		.Handler(...) attaches an http.Handler (instead of a function) to this route.
		http.FileServer(...) returns an http.Handler that knows how to serve files.

		http.FileServer(http.FS(staticFiles)) creates an HTTP handler that serves files from the embedded FS.

		http.FS() converts Go’s generic fs.FS (implemented by embed.FS) into the http.FileSystem interface
		that FileServer expects; This helps wrap it so that HTTP functions can read files from it

		fs.Sub helps create a sub-view of that filesystem that starts inside web/static, since we don't want
		to keep looking up web/static/css/main.css, we just want css/main.css when the browser asks
		for /static/css/main.css; Returns an fs.FS, which is still a virtual file system view

		Handlers in summary will read files from a filesystem and write them to a web response when requested.

		If "css/main.css" was requested, then the "fs := ..." line will:
		1. open that file from the virtual filesystem,
		2. read its contents, and
		3. write the bytes to the browser as the HTTP response


	*/
	staticFiles, err := fs.Sub(staticFS, "web/static")
	if err != nil {
		log.Fatal("Failed to create static sub-filesystem:", err)
	}

	// fs becomes an http.Handler that serves the embedded /web/static/... files from inside the binary
	fs := http.FileServer(http.FS(staticFiles))

	// StripPrefix also matches HTTP request to specific file in the fs variable; URL prior is already stored in r.URL.Path

	/*
		fs = the mailroom worker who can find boxes labeled like "css/main.css".

		stripped = a front desk clerk who checks if your package label starts with "static/", and
		if so, removes that word before handing it to the mailroom.
	*/
	stripped := http.StripPrefix("/static/", fs)

	// We check if the URL prefix contains "/static/" and if it does, we call the "stripped" handler, so we go back up
	s.router.PathPrefix("/static/").Handler(stripped)

	// Page routes registration
	s.router.HandleFunc("/", s.handleIndex).Methods("GET")
	s.router.HandleFunc("/host", s.handleHostDashboard).Methods("GET")
	// Want to note that {code} is like a reverse template where the URL fulfills that variable, but in the handler
	// function we will extract that {code} variable with mux.Vars(r)
	s.router.HandleFunc("/round/{code}", s.handleRoundView).Methods("GET")

	// Api route registration
	// as per it says in the method, this is a subrouter of our 's' Server; All full paths would include /api if not
	api := s.router.PathPrefix("/api").Subrouter()
	api.HandleFunc("/round/create", s.handleCreateRound).Methods("POST")
	api.HandleFunc("/round/join", s.handleJoinRound).Methods("POST")
	api.HandleFunc("/round/{code}/info", s.handleRoundInfo).Methods("GET")
	api.HandleFunc("/round/{code}/state", s.handleUpdateState).Methods("POST")
	api.HandleFunc("/round/{code}/upload", s.handleUpload).Methods("POST")
	api.HandleFunc("/round/{code}/download/{filename}", s.handleDownload).Methods("GET")
	api.HandleFunc("/round/{code}/export", s.handleExport).Methods("GET")
}

// Redis key helper functions
func roundKey(code string) string {
	// keeping this here for reference, but concatenation is faster; Sprintf() helps when mixing different types (%v, etc.)
	return fmt.Sprintf("round:%s", code)
}

func sessionKey(token string) string {
	return fmt.Sprintf("sesssion:%s", token)
}

func fileKey(roundID, filename string) string {
	return fmt.Sprintf("file:%s:%s", roundID, filename)
}

/*
	Some notes:
	http.ResponseWriter will be the pipe back to the user's browser where that variable is used to write the response.
	This response could be anything like HTML, JSON, or any data that you decide to send to it.
	This allows us to do things like:
	w.Write([]byte("Hello!"))           // Send text
	w.WriteHeader(404)                   // Set status code
	w.Header().Set("Content-Type", "application/json")  // Set headers

	http.Request is going to be the variable that contains all the information about the incoming request.
	This could be URLs, headers, cookies, form data, etc.
	It allows us to do things like:

	r.URL.Path           // "/about"
	r.Method            // "GET" or "POST"
	r.Header.Get("Authorization")  // Get a header
	r.FormValue("username")        // Get form data
*/

func generateJoinCode() string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 6) // byte in Go is literally exactly a uint8 (so some number from 0-[2^8 - 1]);
	rand.Read(b)         // each element is a random byte from 0-255
	for i := range b {
		b[i] = charset[b[i]%byte(len(charset))]
	}
	return string(b)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	/*
		So here, we take the template named index.html. index.html or any template for that matter may contain
		some variables that are not hardcoded, and so that's where we would write something to fill variables in
		the index.html or whatever file. We'd usually put it where the nil is in the ExecuteTemplate()
		function parameter. We don't need any dynamic variables at the moment, so that's why we have nil
		for some of the ExecuteTemplate() functions.
	*/

	if err := s.templates.ExecuteTemplate(w, "index.html", nil); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
		log.Printf("Template error: %v", err)
	}
}

func (s *Server) handleHostDashboard(w http.ResponseWriter, r *http.Request) {
	// Todo: check session and verify host
	if err := s.templates.ExecuteTemplate(w, "host.html", nil); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
		log.Printf("Template error: %v", err)
	}
}

func (s *Server) handleRoundView(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r) // mux is the router library; Vars only has path variables in the {} from above.
	// the code carried in the variables of mux.Vars is limited to just this route (whatever code was called with the GET request)
	code := vars["code"]

	cmd1 := s.db.Get(ctx, roundKey(code))
	// separated cmd from result to demo the cmd batching ability (like being able to ask questions and getting multiple answers at once)
	// Very useful when Pipelining commands [can look into that later]
	roundData, err := cmd1.Result()
	if err == redis.Nil {
		http.Error(w, "Round not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Failed to get round", http.StatusInternalServerError)
		return
	}

	var round Round
	if err := json.Unmarshal([]byte(roundData), &round); err != nil {
		http.Error(w, "Failed to parse round data", http.StatusInternalServerError)
		return
	}

	// Check session
	session := s.getSession(r)
	var participant *Participant
	if session != nil && round.Participants != nil {
		participant = round.Participants[session.ParticipantID]
	}

	data := map[string]interface{}{
		"Code":        code,
		"Round":       round,
		"Participant": participant,
	}

	if err := s.templates.ExecuteTemplate(w, "round.html", data); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
		log.Printf("Template error: %v", err)
	}
}

// Api Handler Functions
func (s *Server) handleCreateRound(w http.ResponseWriter, r *http.Request) {
	// Anonymous/lambda struct
	var req struct {
		Name               string    `json:"name"`
		Mode               RoundMode `json:"mode"`
		HostName           string    `json:"hostName"`
		AllowGuestDownload bool      `json:"allowGuestDownload"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	var joinCode string
	for {
		joinCode = generateJoinCode()
		exists, _ := s.db.Exists(ctx, roundKey(joinCode)).Result() // another command and result
		if exists == 0 {
			break
		}
	}

	hostID := uuid.New().String() // just a fun sidenote, UUIDs are like a standard of ID generation (defined by RFC)
	host := &Participant{         // sidenote: This is Go's distinctive type of initialization features.
		ID:          hostID,
		DisplayName: req.HostName,
		IsHost:      true,
		JoinedAt:    time.Now(),
	}

	// Creating the actual round
	round := &Round{
		ID:                 uuid.New().String(),
		Name:               req.Name,
		Mode:               req.Mode,
		JoinCode:           joinCode,
		State:              StateWaiting,
		HostID:             hostID,
		Participants:       map[string]*Participant{hostID: host},
		Submissions:        make(map[string]*Submission),
		AllowGuestDownload: req.AllowGuestDownload,
		CreatedAt:          time.Now(),
	}

	// Storing the round in Redis with a 24-hour expiration timer
	roundData, _ := json.Marshal(round) // Gives back that json byte encoded representation
	if err := s.db.Set(ctx, roundKey(joinCode), roundData, 24*time.Hour).Err(); err != nil {
		http.Error(w, "Failed to create round", http.StatusInternalServerError)
		return
	}

	// Create session
	sessionToken := uuid.New().String()
	session := &Session{
		Token:         sessionToken,
		ParticipantID: hostID,
		RoundCode:     joinCode,
		CreatedAt:     time.Now(),
	}

	sessionData, _ := json.Marshal(session)
	if err := s.db.Set(ctx, sessionKey(sessionToken), sessionData, 24*time.Hour).Err(); err != nil {
		log.Printf("Failed to create session: %v", err)
	}

	// Create upload directory for this round
	os.MkdirAll(filepath.Join("temp/uploads", round.ID), 0755)

	// Setting session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionToken,
		Path:     "/",
		MaxAge:   86400,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	// Return Response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"code":     joinCode,
		"roundId":  round.ID,
		"hostName": req.HostName,
	})
}

func (s *Server) handleJoinRound(w http.ResponseWriter, r *http.Request) {
	// Parse the body of the request
	var req struct { // Anonymous class for request
		Code        string `json:"code"`
		DisplayName string `json:"displayName"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid Request", http.StatusBadRequest)
		return
	}

	// Validation of input and notification to client if failure
	req.Code = strings.ToUpper(strings.TrimSpace(req.Code))
	if req.Code == "" || req.DisplayName == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Code and display name are required",
		})
		return
	}

	// Getting round data from Redis
	roundData, err := s.db.Get(ctx, roundKey(req.Code)).Result()
	if err == redis.Nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Invalid join code",
		})
		return
	} else if err != nil {
		http.Error(w, "Failed to get round", http.StatusInternalServerError)
		return
	}

	// Parsing round data from the binary blob that it was; Also roundData is a Go string, so we gotta convert it into that []byte format
	var round Round
	if err := json.Unmarshal([]byte(roundData), &round); err != nil {
		http.Error(w, "Failed to parse round data", http.StatusInternalServerError)
		return
	}

	// check if the round is still accepting participants
	if round.State != StateWaiting {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "This round is no longer accepting particpants",
		})
		return
	}

	// Check if user already has a session for this round
	existingSession := s.getSession(r)
	var participantID string
	if existingSession != nil && existingSession.RoundCode == req.Code {
		// User is already in this round
		participantID = existingSession.ParticipantID

		// Update their display name if they changed it
		// Just a heads up, "exists" here is a special Go map lookup syntax; when in this second form with the two return variables, the second
		// variable which is the "exists" variable is a bool to check if it exists or not in the map. Very neat and cool syntax imo.
		if participant, exists := round.Participants[participantID]; exists {
			participant.DisplayName = req.DisplayName
		}
	} else {
		// Create a new participant since the session doesn't exists and they don't exist for their own session or the round code is different
		participantID = uuid.New().String()
		participant := &Participant{
			ID:          participantID,
			DisplayName: req.DisplayName,
			IsHost:      false,
			JoinedAt:    time.Now(),
		}

		// Initialize map if nil (shouldn't happen but safety first)
		if round.Participants == nil {
			round.Participants = make(map[string]*Participant)
		}

		// Add the participant to the round
		round.Participants[participantID] = participant
	}

	// Save updated round back to Redis
	updatedRoundData, _ := json.Marshal(round)
	if err := s.db.Set(ctx, roundKey(req.Code), updatedRoundData, 24*time.Hour).Err(); err != nil {
		http.Error(w, "Failed to update round", http.StatusInternalServerError)
		return
	}

	// Create or update session with new session data
	sessionToken := uuid.New().String()
	session := &Session{
		Token:         sessionToken,
		ParticipantID: participantID,
		RoundCode:     req.Code,
		CreatedAt:     time.Now(),
	}

	sessionData, _ := json.Marshal(session)
	if err := s.db.Set(ctx, sessionKey(sessionToken), sessionData, 24*time.Hour).Err(); err != nil {
		log.Printf("Failed to create session: %v", err)
	}

	// Set session cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionToken,
		Path:     "/",
		MaxAge:   86400, // 24 hours in seconds (writing this again)
		HttpOnly: true,  // Can't be accessed by JavaScript (security)
		SameSite: http.SameSiteLaxMode,
	})

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"code":          req.Code,
		"roundId":       round.ID,
		"participantId": participantID,
		"displayName":   req.DisplayName,
		"isHost":        false,
	})
}

func (s *Server) handleUpdateState(w http.ResponseWriter, r *http.Request) {
	// Todo: Need to fully implement still
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(`{"status":"not implemented"}`)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	// Todo: Need to fully implement still
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(`{"status":"not implemented"}`)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleRoundInfo(w http.ResponseWriter, r *http.Request) {
	// Todo: Need to fully implement still
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(`{"status":"not implemented"}`)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	// Todo: Need to fully implement still
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(`{"status":"not implemented"}`)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	// Todo: Need to fully implement still
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(`{"status":"not implemented"}`)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	// Todo: Need to fully implement still
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(`{"status":"not implemented"}`)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func initRDB() *redis.Client {

	redisAddr := os.Getenv("REDIS_URL")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: os.Getenv("REDIS_PASSWORD"), // Default to "" if no password set

		// We only use the first one '0' since we can put the whole lobby structure on there
		DB:       0, // This DB parameter is a namespace;
		Protocol: 3, // This parameter tells us how we want our formatting style (e.g. RESP2, RESP3 [protocol 3 = RESP3])
	})

	// Test Redis connection
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal("Failed to connect to Redis:", err)
	}

	log.Println("Connected to Redis successfully")

	return rdb
}

func (s *Server) getSession(r *http.Request) *Session {
	/* Note for cookies and whatnot:
	- The cookies are created in my SetCookie method above.
	- Cookies should ONLY contains the session token while other sensitive infor is in the DB (passwords, permissions, user data)
	- We use cookies cause HTTP is statesless and every request is independent; Browsers don't remember users, Servers don't remember browsers, and
	  every request could be literally anyone

	If we DIDN'T use cookies then...
		- user joining a round would not be remembered
		- They would refresh and lose their identity
		- I'd have to re-send their participant ID manually every request
		- Guests could impersonate anyone

	The cookies allow us to store the session token so I can look up their identity

	The rest of the data is stored in Redis for all the reasons we listed above and whatnot. We only expose the session token because
	that's the least that we need to track and whatnot. After, we can retrive the full data from Redis when we need, and of course you can see
	that being done below in the unmarshalling line and whatnot.

	*/

	// Because Cookies are stored in the User's browser, it's part of the *http.Request
	cookie, err := r.Cookie("session")
	if err != nil {
		return nil
	}

	sessionData, err := s.db.Get(ctx, sessionKey(cookie.Value)).Result()
	if err != nil {
		return nil
	}

	var session Session
	// Decode from CDR (json), with raw byte data as first parameter and the pointer (needs to be a pointer), to
	// the variable you want to transfer the data to as the second parameter.
	if err := json.Unmarshal([]byte(sessionData), &session); err != nil {
		return nil
	}

	return &session
}
