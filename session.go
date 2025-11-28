package main

import (
	"encoding/json"
	"net/http" // For HTTP server and client funcionality
	"time"
)

type Session struct {
	Token         string    `json:"token"`
	ParticipantID string    `json:"participantId"`
	RoundCode     string    `json:"roundCode"`
	CreatedAt     time.Time `json:"createdAt"`
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

	// Because Cookies are stored in the User's browser, it's part of the *http.Request; gets the "session" named cookie
	cookie, err := r.Cookie("session")
	if err != nil {
		return nil
	}

	sessionData, err := s.db.Get(ctx, sessionKey(cookie.Value)).Result()
	if err != nil {
		return nil
	}

	var session Session
	// Decode from CDR (json) which is in UTF-8 (note: it's a Go string which is already in UTF-8, but we just need to copy it into a byte slice instead which
	// is also UTF-8), with raw byte data as first parameter and the pointer (needs to be a pointer), to
	// the variable you want to transfer the data to as the second parameter.
	// Then now my data from Redis can be unmarshalled into my session struct
	if err := json.Unmarshal([]byte(sessionData), &session); err != nil {
		return nil
	}

	return &session
}
