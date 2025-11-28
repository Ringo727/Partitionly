package main

import (
	"encoding/json"
	"github.com/gorilla/mux" // Router for advanced URL Routing
	"log"                    // For Logging errors and info messages
	"net/http"               // For HTTP server and client funcionality

	"github.com/redis/go-redis/v9"
)

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
