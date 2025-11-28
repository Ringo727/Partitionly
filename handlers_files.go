package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux" // Router for advanced URL Routing
	"io"
	"log"      // For Logging errors and info messages
	"net/http" // For HTTP server and client funcionality
	"os"       // For OS interface
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	code := vars["code"]

	// Get session
	session := s.getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get the round from Redis
	roundData, err := s.db.Get(ctx, roundKey(code)).Result()
	if err == redis.Nil {
		http.Error(w, "Round not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Failed to get round", http.StatusInternalServerError)
		return
	}

	// Parse round data
	var round Round
	if err := json.Unmarshal([]byte(roundData), &round); err != nil {
		http.Error(w, "Failed to parse round data", http.StatusInternalServerError)
		return
	}

	participant, exists := round.Participants[session.ParticipantID]
	if !exists {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "You are not a participant in this round",
		})
		return
	}

	// Check if round is active (only allow uploads during active state)
	if round.State != StateActive {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Uploads are only allowed when the round is active",
		})
		return
	}

	// Sample mode specific: check if sample exists for sample mode
	if round.Mode == ModeSample && round.SampleFileID == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Waiting for host to upload sample file first",
		})
		return
	}

	// Check for existing submission; Allows overwrites to occur
	var isReplacement bool
	var oldSubmission *Submission
	if round.Submissions != nil {
		if existing, hasSubmitted := round.Submissions[session.ParticipantID]; hasSubmitted {
			// User is replacing their submission
			isReplacement = true
			oldSubmission = existing
			log.Printf("User %s (%s) is replacing their submission",
				participant.DisplayName, session.ParticipantID)
		}
	}

	// Below is like a classic file uploading pattern for this language
	// Parse the multipart (from 32 MB max size)
	// 32 << 20 is a bit shift operation where we shift 32 by 20 buts which is the same as multiplying by 2^n
	// likewise, 1 << 20 is 1 MB
	err = r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, "File too large (max 32MB)", http.StatusBadRequest)
		return
	}

	// Get the file from the form
	file, handler, err := r.FormFile("audio")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "No file provided",
		})
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Failed to close file in handleUpload; error: %v", err)
		}
	}()

	// Validating file extension for audio files
	ext := strings.ToLower(filepath.Ext(handler.Filename))
	validExts := map[string]bool{
		".mp3": true, ".wav": true, ".m4a": true,
		".flac": true, ".ogg": true, ".aac": true,
	}
	if !validExts[ext] {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Invalid file type. Please upload an audio file (mp3, wav, m4a, flac, ogg, aac)",
		})
		return
	}

	// Generating a unique filename to avoid collisions
	safeFilename := fmt.Sprintf("%s_%s_%d%s",
		session.ParticipantID, uuid.New().String()[:8],
		time.Now().Unix(),
		ext)

	// Create the full file path
	uploadDir := filepath.Join("temp/uploads", round.ID)
	if err := os.MkdirAll(uploadDir, 0755); err != nil { // Ensure directory exists
		http.Error(w, "Error upon making filepath for upload directory", http.StatusInternalServerError)
		return
	}
	fullPath := filepath.Join(uploadDir, safeFilename)

	// Creating the destination file
	dst, err := os.Create(fullPath)
	if err != nil {
		log.Printf("Failed to create file: %v", err)
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := dst.Close(); err != nil {
			log.Printf("Failed to close destination file in handleUpload; error: %v", err)
		}
	}()

	// Copy the uploaded file to destination
	writtenBytes, err := io.Copy(dst, file)
	if err != nil {
		log.Printf("Failed to write file: %v", err)
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	// Create submission record
	submission := &Submission{
		ParticipantID: session.ParticipantID,
		Filename:      safeFilename,
		OriginalName:  handler.Filename,
		UploadedAt:    time.Now(),
	}

	// Initialize submisions map if nil
	if round.Submissions == nil {
		round.Submissions = make(map[string]*Submission)
	}

	switch round.Mode {
	case ModeSample:
		// In sample mode, everyone remixes the host's sample
		// The sample itself is uploaded via handleUploadSample, not here
		// This handler is just for remixes

		// Nothing special needed here since the sample check already happened above
		// and everyone (including host) can upload their remix
		log.Printf("Sample mode: %s uploaded their remix", participant.DisplayName)

	case ModeTelephone:
		// In telephone mode, it's a chain where each person gets the previous person's upload
		// Create ordered list of ALL participants
		participantIDs := make([]string, 0, len(round.Participants))
		for id := range round.Participants {
			participantIDs = append(participantIDs, id)
		}
		sort.Strings(participantIDs) // Deterministic order

		// Find current participant's position
		currentPos := -1
		for i, id := range participantIDs {
			if id == session.ParticipantID {
				currentPos = i
				break
			}
		}

		// Assign to next participant in chain
		if currentPos != -1 && currentPos < len(participantIDs)-1 {
			nextParticipantID := participantIDs[currentPos+1]
			submission.AssignedToID = nextParticipantID

			// If replacing, keep the same assignment
			if isReplacement && oldSubmission.AssignedToID != "" {
				submission.AssignedToID = oldSubmission.AssignedToID
			}

			log.Printf("Telephone mode: %s's upload assigned to %s",
				participant.DisplayName, round.Participants[submission.AssignedToID].DisplayName)
		}

		// First person's upload becomes the starting point sample we start with that goes through the telephone line
		if currentPos == 0 {
			round.SampleFileID = safeFilename
			log.Printf("Telephone mode: Starting file set by %s", participant.DisplayName)
		}
	}

	// Add/Update submission in round (happens for both modes) to be saved to Redis next
	round.Submissions[session.ParticipantID] = submission

	// Save updated round to Redis
	updatedRoundData, _ := json.Marshal(round)
	if err := s.db.Set(ctx, roundKey(code), updatedRoundData, 24*time.Hour).Err(); err != nil {
		// Try to clean up the uploaded file since we couldn't save to Redis
		if err := os.Remove(fullPath); err != nil {
			http.Error(w, "Failed to remove fullPath", http.StatusInternalServerError)
			return
		}

		http.Error(w, "Failed to update round", http.StatusInternalServerError)
		return
	}

	// DELETE OLD FILE if this was a replacement (AFTER Redis save succeeds)
	if isReplacement && oldSubmission != nil {
		oldPath := filepath.Join("temp/uploads", round.ID, oldSubmission.Filename)
		if err := os.Remove(oldPath); err != nil {
			// Log but don't fail - old file cleanup is not critical
			log.Printf("Warning: Could not delete old file %s: %v", oldPath, err)
		} else {
			log.Printf("Deleted old submission file: %s", oldSubmission.Filename)
		}
	}

	// Log successful upload
	action := "uploaded"
	if isReplacement {
		action = "replaced"
	}
	log.Printf("File %s: %s by %s (%s) - %d bytes",
		action, safeFilename, participant.DisplayName, session.ParticipantID, writtenBytes)

	// Preparing response data
	responseData := map[string]interface{}{
		"success":       true,
		"filename":      safeFilename,
		"originalName":  handler.Filename,
		"size":          writtenBytes,
		"uploadedBy":    participant.DisplayName,
		"isReplacement": isReplacement,
		"message":       "", // initialize empty
	}

	// Confirm success
	switch round.Mode {
	case ModeSample:
		if isReplacement {
			responseData["message"] = "Your remix has been updated successfully!"
		} else {
			responseData["message"] = "Your remix has been uploaded successfully!"
		}

	case ModeTelephone:
		if submission.AssignedToID != "" {
			nextParticipant := round.Participants[submission.AssignedToID]
			responseData["assignedTo"] = nextParticipant.DisplayName

			if isReplacement {
				responseData["message"] = fmt.Sprintf("Your updated upload will be passed to %s", nextParticipant.DisplayName)
			} else {
				responseData["message"] = fmt.Sprintf("Your upload will be passed to %s", nextParticipant.DisplayName)
			}
		} else {
			if isReplacement {
				responseData["message"] = "Your updated upload is the last in the telephone chain!"
			} else {
				responseData["message"] = "Your upload is the last in the telephone chain!"
			}
		}
	}

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(responseData)
}

func (s *Server) handleUploadSample(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	code := vars["code"]

	session := s.getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	roundData, err := s.db.Get(ctx, roundKey(code)).Result()
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

	// MUST be the host
	if session.ParticipantID != round.HostID {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Only the host can upload the sample file",
		})
		return
	}

	// MUST be sample mode
	if round.Mode != ModeSample {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Sample uploads are only for sample mode rounds",
		})
		return
	}

	// Sample can only be changed while in waiting state
	// Once active state, the sample is locked in
	if round.State != StateWaiting {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Sample can only be uploaded or changed before the round starts. Current state: " + string(round.State),
		})
		return
	}

	// Check if this is a replacement
	var isReplacement bool
	var oldSampleFile string
	if round.SampleFileID != "" {
		isReplacement = true
		oldSampleFile = round.SampleFileID
		log.Printf("Host is replacing the sample file (old: %s)", oldSampleFile)
	}

	// Parse multipart form
	err = r.ParseMultipartForm(32 << 20)
	if err != nil {
		http.Error(w, "File too large (max 32MB)", http.StatusBadRequest)
		return
	}

	// Get file from form
	file, handler, err := r.FormFile("sample")
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "No file provided",
		})
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Failed to close file in handleUploadSample; error: %v", err)
		}
	}()

	// Validate audio file extension
	ext := strings.ToLower(filepath.Ext(handler.Filename))
	validExts := map[string]bool{
		".mp3": true, ".wav": true, ".m4a": true,
		".flac": true, ".ogg": true, ".aac": true,
	}
	if !validExts[ext] {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Invalid file type. Please upload an audio file",
		})
		return
	}

	// Generate filename with SAMPLE prefix for clarity
	safeFilename := fmt.Sprintf("SAMPLE_%s_%d%s",
		uuid.New().String()[:8],
		time.Now().Unix(),
		ext)

	// Create file path
	uploadDir := filepath.Join("temp/uploads", round.ID)
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		http.Error(w, "Failed to create upload directory", http.StatusInternalServerError)
		return
	}
	fullPath := filepath.Join(uploadDir, safeFilename)

	// Create destination file
	dst, err := os.Create(fullPath)
	if err != nil {
		log.Printf("Failed to create sample file: %v", err)
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := dst.Close(); err != nil {
			log.Printf("Failed to close destination file; error: %v", err)
		}
	}()

	// Copy uploaded file to destination
	writtenBytes, err := io.Copy(dst, file)
	if err != nil {
		log.Printf("Failed to write sample file: %v", err)
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
	}

	// Update round with sample file ID
	round.SampleFileID = safeFilename

	// Save updated round to Redis
	updatedRoundData, _ := json.Marshal(round)
	if err := s.db.Set(ctx, roundKey(code), updatedRoundData, 24*time.Hour).Err(); err != nil {
		// Clean up file if Redis save failed
		if err := os.Remove(fullPath); err != nil {
			log.Printf("Failed to remove file path and or file; error: %v", err)
		}
		http.Error(w, "Failed to update round", http.StatusInternalServerError)
		return
	}

	// DELETE OLD SAMPLE FILE if this was a replacement (AFTER Redis save succeeds)
	if isReplacement && oldSampleFile != "" {
		oldPath := filepath.Join("temp/uploads", round.ID, oldSampleFile)
		if err := os.Remove(oldPath); err != nil {
			// Log but don't fail - old file cleanup is not critical
			log.Printf("Warning: Could not delete old sample file %s: %v", oldPath, err)
		} else {
			log.Printf("Deleted old sample file: %s", oldSampleFile)
		}
	}

	// Log successful sample upload
	action := "uploaded"
	if isReplacement {
		action = "replaced"
	}
	log.Printf("Sample file %s for round %s: %s (original: %s) - %d bytes",
		action, code, safeFilename, handler.Filename, writtenBytes)

	// Return success response
	responseMessage := "Sample uploaded successfully! Participants can download and create remixes once the round starts."
	if isReplacement {
		responseMessage = "Sample replaced successfully! The new sample will be used when the round starts."
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":       true,
		"filename":      safeFilename,
		"originalName":  handler.Filename,
		"size":          writtenBytes,
		"message":       responseMessage,
		"isReplacement": isReplacement,
	})
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	code := vars["code"]
	requestedFilename := vars["filename"] // From URL: /round/{code}/download/{filename}

	// Get session
	session := s.getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get round from Redis
	roundData, err := s.db.Get(ctx, roundKey(code)).Result()
	if err == redis.Nil {
		http.Error(w, "Round not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Failed to get round", http.StatusInternalServerError)
		return
	}

	// Parse round data
	var round Round
	if err := json.Unmarshal([]byte(roundData), &round); err != nil {
		http.Error(w, "Failed to parse round data", http.StatusInternalServerError)
		return
	}

	// Check if user is a participant
	participant, isParticipant := round.Participants[session.ParticipantID]
	if !isParticipant {
		// Check if guest downloads are allowed
		if !round.AllowGuestDownload {
			http.Error(w, "You must be a participant to download files", http.StatusForbidden)
			return
		}
	}

	// Determine which file the user should be able to download
	var fileToServe string
	var originalName string

	switch round.Mode {
	case ModeSample:
		// In sample mode, participants download the sample (except the host who made it)
		if requestedFilename == "sample" || requestedFilename == round.SampleFileID {
			if round.SampleFileID == "" {
				http.Error(w, "No sample uploaded yet", http.StatusNotFound)
				return
			}
			fileToServe = round.SampleFileID
			originalName = "sample.mp3" // Generic name for sample
		} else {
			// Downloading someone's remix; verify it exists
			// Look for the submission whose stored filename matches the requested one
			// We scan through all submissions and break as soon as we find the match
			for _, submission := range round.Submissions {
				if submission.Filename == requestedFilename {
					fileToServe = submission.Filename
					originalName = submission.OriginalName
					break
				}
			}
		}

	case ModeTelephone:
		// In telephone mode, find the file assigned to this participant
		if requestedFilename == "assigned" {
			// Find the file assigned to this participant
			for _, submission := range round.Submissions {
				if submission.AssignedToID == session.ParticipantID {
					fileToServe = submission.Filename
					originalName = submission.OriginalName
					break
				}
			}

			// Special case: First person gets the original sample if it exists
			if fileToServe == "" && round.SampleFileID != "" {
				// Check if this person is first in chain (after the starter)
				participantIDs := make([]string, 0, len(round.Participants))
				for id := range round.Participants {
					participantIDs = append(participantIDs, id)
				}
				sort.Strings(participantIDs)

				if len(participantIDs) > 1 && participantIDs[1] == session.ParticipantID {
					fileToServe = round.SampleFileID
					originalName = "starting_file.mp3"
				}
			}
		} else {
			// Direct file download by filename (for host/debugging)
			for _, submission := range round.Submissions {
				if submission.Filename == requestedFilename {
					fileToServe = submission.Filename
					originalName = submission.OriginalName
					break
				}
			}
		}
	}

	if fileToServe == "" {
		http.Error(w, "File not found or not available for download", http.StatusNotFound)
		return
	}

	// Build the file path
	filePath := filepath.Join("temp/uploads", round.ID, fileToServe)

	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		log.Printf("Failed to open file %s: %v", filePath, err)
		http.Error(w, "File not found on server", http.StatusNotFound)
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Failed to close file in handleDownload; error: %v", err)
		}
	}()

	fileInfo, err := file.Stat() // Getting file info for size
	if err != nil {
		http.Error(w, "Failed to get file info", http.StatusInternalServerError)
		return
	}

	// Set headers for file download
	w.Header().Set("Content-Type", "audio/mpeg") // Just a generic audio type
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", originalName))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))

	// Just a heads up (no pun intended), the headers need to be set before we write to the body with something like w.Write() [which io.Copy will do too]
	// Stream the file to the response
	written, err := io.Copy(w, file) // Copy from file to w (the response); no need to convert cause files are already in bytes; UTF-8 used for text (ofc, cause we gotta decipher later)
	if err != nil {
		log.Printf("Failed to send file: %v", err)
		return
	}

	log.Printf("File downloaded: %s by %s (%d bytes)", fileToServe, participant.DisplayName, written)

}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	code := vars["code"]

	// Get session
	session := s.getSession(r)
	if session == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Get round from Redis
	roundData, err := s.db.Get(ctx, roundKey(code)).Result()
	if err == redis.Nil {
		http.Error(w, "Round not found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Failed to get round", http.StatusInternalServerError)
		return
	}

	// Parse round data
	var round Round
	if err := json.Unmarshal([]byte(roundData), &round); err != nil {
		http.Error(w, "Failed to parse round data", http.StatusInternalServerError)
		return
	}

	// Check if user is the host (only host can export all)
	if session.ParticipantID != round.HostID {
		http.Error(w, "Only the host can export all files", http.StatusForbidden)
		return
	}

	// Check if there are any submissions to export; Can't export a submission if there are none lol
	if len(round.Submissions) == 0 && round.SampleFileID == "" {
		http.Error(w, "No files to export", http.StatusNotFound)
		return
	}

	// Create a zip file in memory
	// For production with large files, you'd want to stream this or use temp files
	buf := new(bytes.Buffer) // bytes.Buffer is a growable in-memory byte array; It implements both io.Writer and io.Reader
	zipWriter := zip.NewWriter(buf)

	// Add sample file if it exists
	if round.SampleFileID != "" {
		filePath := filepath.Join("temp/uploads", round.ID, round.SampleFileID)
		if err := addFileToZip(zipWriter, filePath, "00_sample_"+round.SampleFileID); err != nil {
			log.Printf("Failed to add sample to zip: %v", err)
		}
	}

	// Add all submissions and sort by participant name for consistent ordering
	type submissionInfo struct {
		ParticipantName string
		Submission      *Submission
	}

	var sortedSubmissions []submissionInfo
	for participantID, submission := range round.Submissions {
		participant := round.Participants[participantID]
		sortedSubmissions = append(sortedSubmissions, submissionInfo{
			ParticipantName: participant.DisplayName,
			Submission:      submission,
		})
	}

	// Sort by participant name; Just some simple compare function for consistent ordering again
	sort.Slice(sortedSubmissions, func(i, j int) bool {
		return sortedSubmissions[i].ParticipantName < sortedSubmissions[j].ParticipantName
	})

	// Add each submission to the zip
	for i, info := range sortedSubmissions {
		filePath := filepath.Join("temp/uploads", round.ID, info.Submission.Filename)
		// Naming files with number prefix for order and participant name for some clarity naming convention
		zipFilename := fmt.Sprintf("%02d_%s_%s", i+1, info.ParticipantName, info.Submission.OriginalName)

		if err := addFileToZip(zipWriter, filePath, zipFilename); err != nil {
			log.Printf("Failed to add file to zip: %v", err)
			// We'll still continue with other files even if one fails
		}
	}

	// Closing the zip writer
	if err := zipWriter.Close(); err != nil {
		http.Error(w, "Failed to create zip file", http.StatusInternalServerError)
		return
	}

	// Set headers for zip download
	zipFilename := fmt.Sprintf("%s_%s_export.zip", round.Name, time.Now().Format("20060102_150405"))
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", zipFilename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", buf.Len()))

	// Send the zip file
	if _, err := w.Write(buf.Bytes()); err != nil {
		log.Printf("Failed to send zip file: %v", err)
		return
	}

	log.Printf("Exported %d files for round %s by host", len(round.Submissions)+1, code) // may be one off because of the sample file for the export count
}

// Helper function for adding a file to a zip
func addFileToZip(zipWriter *zip.Writer, filePath string, zipPath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Failed to close in addFileToZip; error: %v", err)
		}
	}()

	// Get file info
	info, err := file.Stat()
	if err != nil {
		return err
	}

	// Create a zip file header
	header, err := zip.FileInfoHeader(info) // Takes the metadata from the file and makes a ZIP description from it
	if err != nil {
		return err
	}

	// Use the custom zip path (with participant name, etc.)
	header.Name = zipPath       // zipPath is just the name the file should have inside the Zip file
	header.Method = zip.Deflate // Compression

	// Create writer for this file in the zip
	// Basically tells the zip that I'm adding a new file with this metadata that we set above into the zip
	// Establishes like the ghost file in the Zip that is going to take the copied compressed bytes below in the io.Copy()
	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}

	// Copy file content to zip
	// Go handles the transferring and compressing of the file into the compressed writer ghost zip file and fills it up

	/*
		Just an interesting side note:
		io.Copy takes an io.writer as its first parameter right? In doing so, that first parameter ("writer" in my case) fulfills the Writer interface and
		implements a Write method. This is how Copy knows to compress the bytes from "file" into "writer". The "writer" variable has its Write method have
		some compression logic, and io.Copy utilizes this. Pretty neat [and not as magical as I thought with the big into small surface level observation]
	*/
	_, err = io.Copy(writer, file)
	return err
}
