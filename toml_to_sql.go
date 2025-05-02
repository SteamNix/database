package main

import (
    "database/sql"
    "fmt"
    "log"
    "os"

    "github.com/pelletier/go-toml"
    _ "modernc.org/sqlite"
)

type HashData struct {
    Hashes map[string]string `toml:"hashes"` // info_hash -> steam_id
}

func main() {
    // Read TOML file
    data, err := os.ReadFile("hashes.toml")
    if err != nil {
        log.Fatalf("Failed to read TOML file: %v", err)
    }

    var parsed HashData
    if err := toml.Unmarshal(data, &parsed); err != nil {
        log.Fatalf("Failed to parse TOML: %v", err)
    }

    // Open or create SQLite DB using modernc.org/sqlite
    db, err := sql.Open("sqlite", "./hashes.db")
    if err != nil {
        log.Fatalf("Failed to open database: %v", err)
    }
    defer db.Close()

    // Create table
    _, err = db.Exec(`
        CREATE TABLE IF NOT EXISTS hashes (
            info_hash TEXT NOT NULL UNIQUE PRIMARY KEY,
            steam_id TEXT NOT NULL UNIQUE,
            installed BOOLEAN NOT NULL DEFAULT 0
        );
    `)
    if err != nil {
        log.Fatalf("Failed to create table: %v", err)
    }

    // Prepare insert or update statement
    stmt, err := db.Prepare(`
        INSERT INTO hashes(info_hash, steam_id, installed)
        VALUES (?, ?, COALESCE((SELECT installed FROM hashes WHERE info_hash = ?), 0))
        ON CONFLICT(info_hash) DO UPDATE SET steam_id = excluded.steam_id;
    `)
    if err != nil {
        log.Fatalf("Failed to prepare statement: %v", err)
    }
    defer stmt.Close()

    for hash, steamID := range parsed.Hashes {
        _, err := stmt.Exec(hash, steamID, hash)
        if err != nil {
            log.Printf("Insert failed for %s -> %s: %v", hash, steamID, err)
        }
    }

    fmt.Println("TOML data inserted into SQLite successfully.")
}

