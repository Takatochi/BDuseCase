package main

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5/pgxpool"
)

var dbpool *pgxpool.Pool

// Message модель
type Message struct {
	ID      int
	Sender  string
	Content string
	SentAt  time.Time
}

// AuditLog модель
type AuditLog struct {
	Action     string
	Username   string
	TargetType string
	TargetID   int
	CreatedAt  time.Time
}

// Контролер: Показати повідомлення
func viewMessages(w http.ResponseWriter, r *http.Request) {
	chatID := mux.Vars(r)["chatId"]
	rows, err := dbpool.Query(context.Background(), `
		SELECT m.id, u.username, m.content, m.sent_at
		FROM messages m
		JOIN users u ON u.id = m.sender_id
		WHERE m.chat_id = $1 ORDER BY m.sent_at
	`, chatID)
	if err != nil {
		http.Error(w, "DB error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.ID, &msg.Sender, &msg.Content, &msg.SentAt); err != nil {
			http.Error(w, "Scan error", http.StatusInternalServerError)
			return
		}
		messages = append(messages, msg)
	}
	tmpl, _ := template.ParseFiles("templates/index.html")
	tmpl.Execute(w, messages)

	_, _ = dbpool.Exec(context.Background(), `
		INSERT INTO audit_logs (action, user_id, target_type, target_id, created_at)
		VALUES ('view_messages', NULL, 'chat', $1, now())
	`, chatID)
}

// Контролер: Показати логи
func viewLogs(w http.ResponseWriter, r *http.Request) {
	rows, err := dbpool.Query(context.Background(), `
    SELECT a.action, COALESCE(u.username, 'Гість'), a.target_type, a.target_id, a.created_at
    FROM audit_logs a
    LEFT JOIN users u ON a.user_id = u.id
    ORDER BY a.created_at DESC LIMIT 50
	`)
	if err != nil {
		http.Error(w, "Log error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var logs []AuditLog
	for rows.Next() {
		var logEntry AuditLog
		if err := rows.Scan(&logEntry.Action, &logEntry.Username, &logEntry.TargetType, &logEntry.TargetID, &logEntry.CreatedAt); err != nil {
			http.Error(w, "Log scan error", http.StatusInternalServerError)
			return
		}
		logs = append(logs, logEntry)
	}
	tmpl, _ := template.ParseFiles("templates/logs.html")
	tmpl.Execute(w, logs)
}

// Контролер: Додати повідомлення
func addMessage(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Parse error", http.StatusBadRequest)
		return
	}
	senderID, _ := strconv.Atoi(r.FormValue("sender_id"))
	chatID, _ := strconv.Atoi(r.FormValue("chat_id"))
	content := r.FormValue("content")

	var messageID int
	err := dbpool.QueryRow(context.Background(), `
		INSERT INTO messages (sender_id, chat_id, content, full_text_search)
		VALUES ($1, $2, $3, to_tsvector('simple', $3)) RETURNING id
	`, senderID, chatID, content).Scan(&messageID)
	if err != nil {
		http.Error(w, "DB insert error", http.StatusInternalServerError)
		return
	}

	_, _ = dbpool.Exec(context.Background(), `
		INSERT INTO audit_logs (action, user_id, target_type, target_id, created_at)
		VALUES ('add_message', $1, 'message', $2, now())
	`, senderID, messageID)

	http.Redirect(w, r, "/chat/1", http.StatusSeeOther)
}

func main() {
	var err error
	dbpool, err = pgxpool.New(context.Background(), "postgres://admin:admin@localhost:5432/mydatabase")
	if err != nil {
		log.Fatal("DB connection failed:", err)
	}
	defer dbpool.Close()

	r := mux.NewRouter()
	r.HandleFunc("/chat/{chatId}", viewMessages).Methods("GET")
	r.HandleFunc("/send", addMessage).Methods("POST")
	r.HandleFunc("/logs", viewLogs).Methods("GET")
	r.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	log.Println("Web App: http://localhost:8080/chat/1")
	http.ListenAndServe(":8080", r)
}
