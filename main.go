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
		that FileServer expects.

	*/
	staticFiles, err := fs.Sub(staticFS, "web/static")
	if err != nil {
		log.Fatal("Failed to create static sub-filesystem:", err)
	}

	fs := http.FileServer(http.FS(staticFiles))
	stripped := http.StripPrefix("/static/", fs)
	s.router.PathPrefix("/static/").Handler(stripped)
}
