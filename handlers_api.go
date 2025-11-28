package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux" // Router for advanced URL Routing
	"log"                    // For Logging errors and info messages
	"net/http"               // For HTTP server and client funcionality
	"os"                     // For OS interface
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

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
	if err := os.MkdirAll(filepath.Join("temp/uploads", round.ID), 0755); err != nil {
		// Shouldn't happen because we have this folder already created, but error checks are good
		log.Printf("Failed to create upload directory for temp/uploads with err: %v", err)
	}

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
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"code":     joinCode,
		"roundId":  round.ID,
		"hostName": req.HostName,
	}); err != nil {
		log.Printf("Failed to encode return json response for round creation handler; err: %v", err)
	}
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
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Code and display name are required",
		}); err != nil {
			log.Printf("Failed to encode json for validation of display name and round code; err: %v", err)
		}
		return
	}

	// Getting round data from Redis
	roundData, err := s.db.Get(ctx, roundKey(req.Code)).Result()
	if err == redis.Nil {
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Invalid join code",
		}); err != nil {
			log.Printf("Failed to encode json for invalid join code; err: %v", err)
		}
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
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "This round is no longer accepting particpants",
		}); err != nil {
			log.Printf("Failed to encode json for round no longer accepting participants; err: %v", err)
		}
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

		// This is to prevent CSRF (cross-site request forgery) attacks.
		// Limits cookies to be delivered by links clicked on other websites and also by refreshing
		// Usually there's also a "Secure:" flag too, but now there's not because it's usually when SameSite=None
		SameSite: http.SameSiteLaxMode,
	})

	// Return success response; parts like this are for the frontend
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"code":          req.Code,
		"roundId":       round.ID,
		"participantId": participantID,
		"displayName":   req.DisplayName,
		"isHost":        false,
	}); err != nil {
		log.Printf("Failed to encode json for success response for handleJoinRound; err: %v", err)
	}
}

func (s *Server) handleUpdateState(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	code := vars["code"]

	// Parsing body to determine state
	var req struct {
		State RoundState `json:"state"`
	}
	// Where we get the POST request to change state
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}

	// Validate the state transition
	if req.State != StateWaiting && req.State != StateActive && req.State != StateClosed {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Invalid state",
		}); err != nil {
			log.Printf("Failed to encode json for state transition validation; err: %v", err)
		}
		return
	}

	// Get session to check if user is host
	session := s.getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	roundData, err := s.db.Get(ctx, roundKey(code)).Result()
	if err != redis.Nil {
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

	// Check if user is the host
	if session.ParticipantID != round.HostID {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Only the host can change the round state",
		}); err != nil {
			log.Printf("Failed to encode json for checking if user is host; err: %v", err)
		}
		return
	}

	// Updating the state
	oldState := round.State
	round.State = req.State

	// Saving new updated round back to Redis
	updatedRoundData, _ := json.Marshal(round)
	if err := s.db.Set(ctx, roundKey(code), updatedRoundData, 24*time.Hour).Err(); err != nil {
		http.Error(w, "Failed to update round", http.StatusInternalServerError)
		return
	}

	// Printing state change to log
	log.Printf("Round %s state changed from %s to %s by host %s", code, oldState, req.State, session.ParticipantID)

	// Returning success response
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"oldState": oldState,
		"newState": req.State,
		"message":  fmt.Sprintf("Round state updated to %s", req.State),
	}); err != nil {
		log.Printf("Failed to encode json for returning success response for handleUpdateState; err: %v", err)
	}
}

func (s *Server) handleRoundInfo(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	code := vars["code"]

	// Getting round from Redis
	roundData, err := s.db.Get(ctx, roundKey(code)).Result()
	if err != nil {
		http.Error(w, "Round not found", http.StatusNotFound)
		return
	}

	var round Round
	json.Unmarshal([]byte(roundData), &round)

	// Returning as JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(round)
}
