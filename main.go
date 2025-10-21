package main

import (
	"database/sql"  // For db interface for SQL dbs
	"embed"         // Allows embedding files into binary at compile time
	"html/template" // HTML templating engine for rendering dynamic web pages
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
