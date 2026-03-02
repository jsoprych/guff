package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStorage implements Storage using SQLite.
type SQLiteStorage struct {
	db *sql.DB
}

// NewSQLiteStorage creates a new SQLite storage backend.
func NewSQLiteStorage(dataDir string) (*SQLiteStorage, error) {
	dbPath := filepath.Join(dataDir, "chat.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}

	s := &SQLiteStorage{db: db}
	if err := s.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}
	return s, nil
}

// initSchema creates the necessary tables if they don't exist.
func (s *SQLiteStorage) initSchema() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT,
			model_name TEXT NOT NULL,
			title TEXT,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			message_count INTEGER NOT NULL DEFAULT 0,
			total_tokens INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			tool_calls TEXT,
			created_at TIMESTAMP NOT NULL,
			token_count INTEGER,
			FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS state_files (
			id TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			path TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL,
			token_count INTEGER NOT NULL,
			FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_session_id ON messages(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_updated_at ON sessions(updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_state_files_session_id ON state_files(session_id)`,
	}

	for _, q := range queries {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("failed to execute query %q: %w", q, err)
		}
	}
	return nil
}

// Close implements Storage.Close.
func (s *SQLiteStorage) Close() error {
	return s.db.Close()
}

// CreateSession implements Storage.CreateSession.
func (s *SQLiteStorage) CreateSession(ctx context.Context, session *Session) error {
	query := `INSERT INTO sessions (id, user_id, model_name, title, created_at, updated_at, message_count, total_tokens)
	          VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query,
		session.ID,
		session.UserID,
		session.ModelName,
		session.Title,
		session.CreatedAt,
		session.UpdatedAt,
		session.MessageCount,
		session.TotalTokens,
	)
	return err
}

// GetSession implements Storage.GetSession.
func (s *SQLiteStorage) GetSession(ctx context.Context, id string) (*Session, error) {
	query := `SELECT id, user_id, model_name, title, created_at, updated_at, message_count, total_tokens
	          FROM sessions WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)
	var session Session
	err := row.Scan(
		&session.ID,
		&session.UserID,
		&session.ModelName,
		&session.Title,
		&session.CreatedAt,
		&session.UpdatedAt,
		&session.MessageCount,
		&session.TotalTokens,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &session, err
}

// UpdateSession implements Storage.UpdateSession.
func (s *SQLiteStorage) UpdateSession(ctx context.Context, session *Session) error {
	query := `UPDATE sessions SET model_name = ?, title = ?, updated_at = ?, message_count = ?, total_tokens = ?
	          WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query,
		session.ModelName,
		session.Title,
		session.UpdatedAt,
		session.MessageCount,
		session.TotalTokens,
		session.ID,
	)
	return err
}

// DeleteSession implements Storage.DeleteSession.
func (s *SQLiteStorage) DeleteSession(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", id)
	return err
}

// ListSessions implements Storage.ListSessions.
func (s *SQLiteStorage) ListSessions(ctx context.Context, userID string, limit, offset int) ([]*Session, error) {
	var rows *sql.Rows
	var err error
	if userID == "" {
		if limit <= 0 {
			query := `SELECT id, user_id, model_name, title, created_at, updated_at, message_count, total_tokens
			          FROM sessions ORDER BY updated_at DESC`
			rows, err = s.db.QueryContext(ctx, query)
		} else {
			query := `SELECT id, user_id, model_name, title, created_at, updated_at, message_count, total_tokens
			          FROM sessions ORDER BY updated_at DESC LIMIT ? OFFSET ?`
			rows, err = s.db.QueryContext(ctx, query, limit, offset)
		}
	} else if limit <= 0 {
		query := `SELECT id, user_id, model_name, title, created_at, updated_at, message_count, total_tokens
		          FROM sessions WHERE user_id = ? ORDER BY updated_at DESC`
		rows, err = s.db.QueryContext(ctx, query, userID)
	} else {
		query := `SELECT id, user_id, model_name, title, created_at, updated_at, message_count, total_tokens
		          FROM sessions WHERE user_id = ? ORDER BY updated_at DESC LIMIT ? OFFSET ?`
		rows, err = s.db.QueryContext(ctx, query, userID, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		var session Session
		if err := rows.Scan(
			&session.ID,
			&session.UserID,
			&session.ModelName,
			&session.Title,
			&session.CreatedAt,
			&session.UpdatedAt,
			&session.MessageCount,
			&session.TotalTokens,
		); err != nil {
			return nil, err
		}
		sessions = append(sessions, &session)
	}
	return sessions, rows.Err()
}

// AddMessage implements Storage.AddMessage.
func (s *SQLiteStorage) AddMessage(ctx context.Context, message *Message) error {
	// JSON tool_calls will be stored as text; we'll handle marshaling separately
	toolCallsJSON := "" // TODO: implement JSON marshaling
	query := `INSERT INTO messages (id, session_id, role, content, tool_calls, created_at, token_count)
	          VALUES (?, ?, ?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query,
		message.ID,
		message.SessionID,
		string(message.Role),
		message.Content,
		toolCallsJSON,
		message.CreatedAt,
		message.TokenCount,
	)
	return err
}

// GetMessage implements Storage.GetMessage.
func (s *SQLiteStorage) GetMessage(ctx context.Context, id string) (*Message, error) {
	query := `SELECT id, session_id, role, content, tool_calls, created_at, token_count
	          FROM messages WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)
	var msg Message
	var roleStr string
	var toolCallsJSON string
	err := row.Scan(
		&msg.ID,
		&msg.SessionID,
		&roleStr,
		&msg.Content,
		&toolCallsJSON,
		&msg.CreatedAt,
		&msg.TokenCount,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	msg.Role = MessageRole(roleStr)
	// TODO: unmarshal toolCallsJSON
	return &msg, nil
}

// GetMessages implements Storage.GetMessages.
func (s *SQLiteStorage) GetMessages(ctx context.Context, sessionID string, limit, offset int) ([]*Message, error) {
	var rows *sql.Rows
	var err error
	if limit <= 0 {
		query := `SELECT id, session_id, role, content, tool_calls, created_at, token_count
		          FROM messages WHERE session_id = ? ORDER BY created_at ASC`
		rows, err = s.db.QueryContext(ctx, query, sessionID)
	} else {
		query := `SELECT id, session_id, role, content, tool_calls, created_at, token_count
		          FROM messages WHERE session_id = ? ORDER BY created_at ASC LIMIT ? OFFSET ?`
		rows, err = s.db.QueryContext(ctx, query, sessionID, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*Message
	for rows.Next() {
		var msg Message
		var roleStr string
		var toolCallsJSON string
		if err := rows.Scan(
			&msg.ID,
			&msg.SessionID,
			&roleStr,
			&msg.Content,
			&toolCallsJSON,
			&msg.CreatedAt,
			&msg.TokenCount,
		); err != nil {
			return nil, err
		}
		msg.Role = MessageRole(roleStr)
		// TODO: unmarshal toolCallsJSON
		messages = append(messages, &msg)
	}
	return messages, rows.Err()
}

// UpdateMessage implements Storage.UpdateMessage.
func (s *SQLiteStorage) UpdateMessage(ctx context.Context, message *Message) error {
	toolCallsJSON := "" // TODO: implement JSON marshaling
	query := `UPDATE messages SET role = ?, content = ?, tool_calls = ?, token_count = ?
	          WHERE id = ?`
	_, err := s.db.ExecContext(ctx, query,
		string(message.Role),
		message.Content,
		toolCallsJSON,
		message.TokenCount,
		message.ID,
	)
	return err
}

// DeleteMessage implements Storage.DeleteMessage.
func (s *SQLiteStorage) DeleteMessage(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM messages WHERE id = ?", id)
	return err
}

// DeleteMessages implements Storage.DeleteMessages.
func (s *SQLiteStorage) DeleteMessages(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM messages WHERE session_id = ?", sessionID)
	return err
}

// CountMessages implements Storage.CountMessages.
func (s *SQLiteStorage) CountMessages(ctx context.Context, sessionID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM messages WHERE session_id = ?", sessionID).Scan(&count)
	return count, err
}

// AddStateFile implements Storage.AddStateFile.
func (s *SQLiteStorage) AddStateFile(ctx context.Context, state *StateFile) error {
	query := `INSERT INTO state_files (id, session_id, path, created_at, token_count)
	          VALUES (?, ?, ?, ?, ?)`
	_, err := s.db.ExecContext(ctx, query,
		state.ID,
		state.SessionID,
		state.Path,
		state.CreatedAt,
		state.TokenCount,
	)
	return err
}

// GetStateFile implements Storage.GetStateFile.
func (s *SQLiteStorage) GetStateFile(ctx context.Context, id string) (*StateFile, error) {
	query := `SELECT id, session_id, path, created_at, token_count FROM state_files WHERE id = ?`
	row := s.db.QueryRowContext(ctx, query, id)
	var state StateFile
	err := row.Scan(
		&state.ID,
		&state.SessionID,
		&state.Path,
		&state.CreatedAt,
		&state.TokenCount,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &state, err
}

// GetStateFiles implements Storage.GetStateFiles.
func (s *SQLiteStorage) GetStateFiles(ctx context.Context, sessionID string) ([]*StateFile, error) {
	query := `SELECT id, session_id, path, created_at, token_count FROM state_files
	          WHERE session_id = ? ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, query, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []*StateFile
	for rows.Next() {
		var file StateFile
		if err := rows.Scan(
			&file.ID,
			&file.SessionID,
			&file.Path,
			&file.CreatedAt,
			&file.TokenCount,
		); err != nil {
			return nil, err
		}
		files = append(files, &file)
	}
	return files, rows.Err()
}

// DeleteStateFile implements Storage.DeleteStateFile.
func (s *SQLiteStorage) DeleteStateFile(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM state_files WHERE id = ?", id)
	return err
}

// DeleteStateFiles implements Storage.DeleteStateFiles.
func (s *SQLiteStorage) DeleteStateFiles(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM state_files WHERE session_id = ?", sessionID)
	return err
}

// CleanupOldStateFiles implements Storage.CleanupOldStateFiles.
func (s *SQLiteStorage) CleanupOldStateFiles(ctx context.Context, olderThan time.Time) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM state_files WHERE created_at < ?", olderThan)
	return err
}
