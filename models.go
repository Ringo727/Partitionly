package main

import (
	"github.com/gorilla/mux" // Router for advanced URL Routing
	"html/template"          // HTML templating engine for rendering dynamic web pages
	"time"

	"github.com/redis/go-redis/v9"
)

type RoundMode string

const (
	ModeSample    RoundMode = "sample"    // Everyone doanloads the same sample file
	ModeTelephone RoundMode = "telephone" // each person gets the previous person's upload
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

type Server struct {
	db        *redis.Client      // Pointer to database connection
	templates *template.Template // parsed HTML templates
	router    *mux.Router        //HTTP router for handling different URLs
}
