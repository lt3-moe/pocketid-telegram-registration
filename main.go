package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type UserCreateData struct {
	FirstName     string
	LastName      string
	DisplayName   string
	Email         string
	EmailVerified bool
	Username      string
	IsAdmin       bool
	Disabled      bool
}

func getUserParams(user *models.User) UserCreateData {
	var username string
	if user.Username != "" {
		username = user.Username
	} else {
		username = fmt.Sprintf("id%d", user.ID)
	}

	return UserCreateData{
		Username:      username,
		Email:         fmt.Sprintf("id%d@tg.lt3.moe", user.ID),
		FirstName:     user.FirstName,
		LastName:      user.LastName,
		DisplayName:   strings.TrimSpace(fmt.Sprintf("%s %s", user.FirstName, user.LastName)),
		EmailVerified: true,
		IsAdmin:       false,
		Disabled:      false,
	}
}

func onStart(ctx context.Context, bot *bot.Bot, update *models.Update) {
	log.Printf("hello from %s\n", update.Message.From.Username)
	params := getUserParams(update.Message.From)

	if err := createPocketUser(ctx, params); err != nil {
		log.Printf("failed to create pocket user: %v", err)
	} else {
		log.Printf("created pocket user: %s", params.Username)
	}
}

func createPocketUser(ctx context.Context, data UserCreateData) error {
	root := strings.TrimSpace(os.Getenv("POCKETID_URL"))
	token := strings.TrimSpace(os.Getenv("POCKETID_TOKEN"))
	if root == "" || token == "" {
		return fmt.Errorf("POCKETID_URL and POCKETID_TOKEN are required")
	}

	// Ensure we POST to the /api/users endpoint on the provided root URL.
	endpoint := strings.TrimRight(root, "/") + "/api/users"

	body, err := json.Marshal(data)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// Use X-API-KEY header for the PocketId API key
	req.Header.Set("X-API-KEY", token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("non-2xx response: %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	return nil
}

const helpMessage = "Type /start to register in <3 PocketId. I will give you a one-time link to add a new passkey."

func onOther(ctx context.Context, b *bot.Bot, update *models.Update) {
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   helpMessage,
	}); err != nil {
		log.Printf("failed to send help message! %v", err)
	}
}

func main() {
	token := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN"))

	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is required")
	}

	opts := []bot.Option{
		bot.WithDefaultHandler(onOther),
	}

	b, err := bot.New(token, opts...)
	if err != nil {
		log.Fatalf("failed to create telegram bot: %v", err)
	}

	me, err := b.GetMe(context.TODO())

	if err != nil {
		log.Fatalf("error setting up: %v", err)
	} else {
		log.Printf("resolved myself to @%s\n", me.Username)
	}

	b.RegisterHandler(bot.HandlerTypeMessageText, "/start", bot.MatchTypeExact, onStart)

	go b.Start(context.TODO())

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		redirectURL := fmt.Sprintf("https://t.me/%s?start=register", me.Username)
		http.Redirect(w, r, redirectURL, http.StatusFound)
	})

	addr := ":8080"
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("http server failed: %v", err)
	}
}
