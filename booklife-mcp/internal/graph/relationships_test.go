package graph

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TestGraphBuilderIntegration tests the relationship graph builder with real database
func TestGraphBuilderIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create temporary database with full schema
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	defer db.Close()

	// Create history and enrichment tables
	_, err = db.Exec(`
		CREATE TABLE history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			author TEXT NOT NULL,
			isbn TEXT,
			timestamp INTEGER NOT NULL,
			activity TEXT NOT NULL,
			details TEXT,
			library TEXT NOT NULL,
			library_key TEXT NOT NULL,
			format TEXT NOT NULL,
			cover_url TEXT,
			color TEXT
		);

		CREATE TABLE book_enrichment (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			history_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			author TEXT NOT NULL,
			openlibrary_id TEXT,
			googlebooks_id TEXT,
			description TEXT,
			themes TEXT,
			topics TEXT,
			mood TEXT,
			complexity TEXT,
			series_name TEXT,
			series_position REAL,
			series_total INTEGER,
			enrichment_sources TEXT,
			enriched_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(history_id)
		);

		CREATE TABLE book_relationships (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			from_history_id INTEGER NOT NULL,
			to_history_id INTEGER NOT NULL,
			relationship_type TEXT NOT NULL,
			strength REAL DEFAULT 1.0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (from_history_id) REFERENCES history(id),
			FOREIGN KEY (to_history_id) REFERENCES history(id),
			UNIQUE(from_history_id, to_history_id, relationship_type)
		);
	`)
	if err != nil {
		t.Fatalf("Failed to create tables: %v", err)
	}

	// Insert test data - same author, different books
	books := []struct {
		id, timestamp int
		title, author string
	}{
		{1, 1000, "The Fellowship of the Ring", "J.R.R. Tolkien"},
		{2, 2000, "The Two Towers", "J.R.R. Tolkien"},
		{3, 3000, "The Return of the King", "J.R.R. Tolkien"},
		{4, 4000, "The Hobbit", "J.R.R. Tolkien"},
		{5, 5000, "1984", "George Orwell"},
		{6, 6000, "Animal Farm", "George Orwell"},
		{7, 7000, "The Da Vinci Code", "Dan Brown"},
	}

	for _, b := range books {
		_, err := db.Exec(`
			INSERT INTO history (id, title, author, timestamp, activity, library, library_key, format)
			VALUES (?, ?, ?, ?, 'Returned', 'Test Library', 'key1', 'ebook')
		`, b.id, b.title, b.author, b.timestamp)
		if err != nil {
			t.Fatalf("Failed to insert book %d: %v", b.id, err)
		}
	}

	builder := NewBuilder(db)
	ctx := context.Background()

	t.Run("BuildSameAuthorRelationships", func(t *testing.T) {
		// Build relationships for first book
		err := builder.BuildRelationships(ctx, 1)
		if err != nil {
			t.Fatalf("BuildRelationships failed: %v", err)
		}

		// Check that same-author relationships were created
		rows, err := db.Query(`
			SELECT from_history_id, to_history_id, relationship_type, strength
			FROM book_relationships
			WHERE from_history_id = 1 AND relationship_type = 'same_author'
		`)
		if err != nil {
			t.Fatalf("Failed to query relationships: %v", err)
		}
		defer rows.Close()

		count := 0
		for rows.Next() {
			var fromID, toID int
			var relType string
			var strength float64
			if err := rows.Scan(&fromID, &toID, &relType, &strength); err != nil {
				t.Fatalf("Failed to scan row: %v", err)
			}
			count++
			t.Logf("  Relationship: %d -> %d (%s, strength=%.2f)", fromID, toID, relType, strength)
		}

		if count == 0 {
			t.Error("Expected same-author relationships to be created")
		} else {
			t.Logf("✓ Created %d same-author relationships", count)
		}
	})

	t.Run("GetRelatedBooks", func(t *testing.T) {
		// Build all relationships first
		err := builder.BuildAllRelationships(ctx)
		if err != nil {
			t.Logf("Warning: BuildAllRelationships failed: %v", err)
		}

		// Test getting related books
		related, err := builder.GetRelatedBooks(ctx, 1, "same_author", 10)
		if err != nil {
			t.Fatalf("GetRelatedBooks failed: %v", err)
		}

		t.Logf("✓ Found %d related books (same_author)", len(related))

		// Verify we found Tolkien's other books
		if len(related) == 0 {
			t.Error("Expected to find related books")
		}

		for _, r := range related {
			t.Logf("  - %s by %s (%.0f%%)", r.Title, r.Author, r.Strength*100)
			if r.RelationshipType != "same_author" {
				t.Errorf("Expected same_author, got %s", r.RelationshipType)
			}
		}
	})

	t.Run("GetRelationships", func(t *testing.T) {
		relationships, err := builder.GetRelationships(ctx, 1)
		if err != nil {
			t.Fatalf("GetRelationships failed: %v", err)
		}

		t.Logf("✓ Found %d total relationships for book 1", len(relationships))

		for _, r := range relationships {
			t.Logf("  %s: %d -> %d (%.2f)", r.RelationshipType, r.FromHistoryID, r.ToHistoryID, r.Strength)
		}
	})
}

// TestGraphBuilderSeriesRelationships tests series relationship building
func TestGraphBuilderSeriesRelationships(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}
	defer db.Close()

	// Create tables
	_, err = db.Exec(`
		CREATE TABLE history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			author TEXT NOT NULL,
			isbn TEXT,
			timestamp INTEGER NOT NULL,
			activity TEXT NOT NULL,
			details TEXT,
			library TEXT NOT NULL,
			library_key TEXT NOT NULL,
			format TEXT NOT NULL,
			cover_url TEXT,
			color TEXT
		);

		CREATE TABLE book_enrichment (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			history_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			author TEXT NOT NULL,
			openlibrary_id TEXT,
			googlebooks_id TEXT,
			description TEXT,
			themes TEXT,
			topics TEXT,
			mood TEXT,
			complexity TEXT,
			series_name TEXT,
			series_position REAL,
			series_total INTEGER,
			enrichment_sources TEXT,
			enriched_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(history_id)
		);

		CREATE TABLE book_relationships (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			from_history_id INTEGER NOT NULL,
			to_history_id INTEGER NOT NULL,
			relationship_type TEXT NOT NULL,
			strength REAL DEFAULT 1.0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (from_history_id) REFERENCES history(id),
			FOREIGN KEY (to_history_id) REFERENCES history(id),
			UNIQUE(from_history_id, to_history_id, relationship_type)
		);
	`)
	if err != nil {
		t.Fatalf("Failed to create tables: %v", err)
	}

	// Insert test data - series books with enrichment
	seriesBooks := []struct {
		id, timestamp             int
		title, author, seriesName string
		seriesPos                 float64
	}{
		{1, 1000, "The Fellowship of the Ring", "J.R.R. Tolkien", "The Lord of the Rings", 1},
		{2, 2000, "The Two Towers", "J.R.R. Tolkien", "The Lord of the Rings", 2},
		{3, 3000, "The Return of the King", "J.R.R. Tolkien", "The Lord of the Rings", 3},
	}

	for _, b := range seriesBooks {
		_, err := db.Exec(`
			INSERT INTO history (id, title, author, timestamp, activity, library, library_key, format)
			VALUES (?, ?, ?, ?, 'Returned', 'Test Library', 'key1', 'ebook')
		`, b.id, b.title, b.author, b.timestamp)
		if err != nil {
			t.Fatalf("Failed to insert book %d: %v", b.id, err)
		}

		// Add enrichment with series info
		_, err = db.Exec(`
			INSERT INTO book_enrichment (history_id, title, author, series_name, series_position, series_total)
			VALUES (?, ?, ?, ?, ?, ?)
		`, b.id, b.title, b.author, b.seriesName, b.seriesPos, 3)
		if err != nil {
			t.Fatalf("Failed to insert enrichment for book %d: %v", b.id, err)
		}
	}

	builder := NewBuilder(db)
	ctx := context.Background()

	// Build relationships for first book
	err = builder.BuildRelationships(ctx, 1)
	if err != nil {
		t.Fatalf("BuildRelationships failed: %v", err)
	}

	// Check series relationships
	rows, err := db.Query(`
		SELECT br.from_history_id, br.to_history_id, br.relationship_type, br.strength,
		       h1.title, h2.title
		FROM book_relationships br
		INNER JOIN history h1 ON br.from_history_id = h1.id
		INNER JOIN history h2 ON br.to_history_id = h2.id
		WHERE br.from_history_id = 1 AND br.relationship_type = 'same_series'
	`)
	if err != nil {
		t.Fatalf("Failed to query relationships: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var fromID, toID int
		var relType string
		var strength float64
		var fromTitle, toTitle string
		if err := rows.Scan(&fromID, &toID, &relType, &strength, &fromTitle, &toTitle); err != nil {
			t.Fatalf("Failed to scan row: %v", err)
		}
		count++
		t.Logf("  ✓ Series relationship: %s -> %s (%.2f)", fromTitle, toTitle, strength)
	}

	if count == 0 {
		t.Error("Expected same-series relationships to be created")
	} else {
		t.Logf("✓ Created %d series relationships", count)
	}
}
