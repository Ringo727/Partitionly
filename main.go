package main

import (
	"database/sql"  // For db interface for SQL dbs
	"embed"         // Allows embedding files into binary at compile time
	"html/template" // HTML templating engine for rendering dynamic web pages
	"io/fs"         // Gives FS utilities; fs.Sub() lets us serve from the "web/static" subdirectory
	"log"           // For Logging errors and info messages
	"net/http"      // For HTTP server and client funcionality
	"os"            // For OS interface

	"github.com/gorilla/mux"        // Router for advanced URL Routing
	_ "github.com/mattn/go-sqlite3" //SQLite Driver (underscore means we only need its init used with database/sql)
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

type Server struct {
	db        *sql.DB            // Pointer to database connection
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

	if err := os.MkdirAll("data/uploads", 0755); err != nil {
		log.Fatal("Failed to create data directories:", err)
	}

	db, err := initDB()
	if err != nil {
		log.Fatal("Failed to initialize database:", err)
	}
	defer db.Close() // "defer" ensures db.close() runs when main() exits (cleanup)

	// ParseFS reads from the embedded FS that we created earlier here; We parse all embeddded HTML templates into memory
	templates, err := template.ParseFS(templatesFS, "web/templates/*.html")

	// Initializing new server (we ofc want a pointer because all those member vars are shared resources; Not
	// good to be copying large structs around either and also wouldn't make sense to)
	server := &Server{
		db:        db,
		templates: templates,
		router:    mux.NewRouter(),
	}

	// This uses the function below to register URL paths and link them to their handler functions
	server.setupRoutes()

	log.Println("Server starting on http://localhost:8080")
	if err := http.ListenAndServe(":8080", server.router); err != nil {
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

	// Page routes
	s.router.HandleFunc("/", s.handleIndex).Methods("GET")
	s.router.HandleFunc("/host", s.handleHostDashboard).Methods("GET")
	s.router.HandleFunc("/round/{code}", s.handleRoundView).Methods("GET")
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
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
	// Todo: implement function, get round by code, check participant session
}
