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

	"net/url"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
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

// PocketUser represents the user object returned by PocketId API
type PocketUser struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"emailVerified"`
	FirstName     string `json:"firstName"`
	LastName      string `json:"lastName"`
	DisplayName   string `json:"displayName"`
	IsAdmin       bool   `json:"isAdmin"`
	Disabled      bool   `json:"disabled"`
}

type pocketUserSearchResponse struct {
	Data []PocketUser `json:"data"`
}

// searchPocketUser searches PocketId for a user matching the provided email.
// Returns the first matching user or (nil, nil) if none found.
func searchPocketUser(ctx context.Context, query string) (*PocketUser, error) {
	root := strings.TrimRight(appConfig.PocketIDURL, "/")
	endpoint := root + "/api/users"

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("pagination[limit]", "20")
	q.Set("pagination[page]", "1")
	q.Set("search", query)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-KEY", appConfig.PocketIDToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("pocket user search error: status=%d headers=%v body=%s", resp.StatusCode, resp.Header, strings.TrimSpace(string(respBody)))
		return nil, fmt.Errorf("non-2xx response: %d", resp.StatusCode)
	}

	var out pocketUserSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	if len(out.Data) == 0 {
		return nil, nil
	}

	return &out.Data[0], nil
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

// Config holds all runtime configuration read from environment or .env
type Config struct {
	TelegramBotToken string
	PocketIDURL      string
	PocketIDToken    string
	ListenAddr       string
}

var appConfig Config

func loadConfig() (Config, error) {
	// Load .env if present; ignore error if file missing
	_ = godotenv.Load()

	cfg := Config{
		TelegramBotToken: strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")),
		PocketIDURL:      strings.TrimSpace(os.Getenv("POCKETID_URL")),
		PocketIDToken:    strings.TrimSpace(os.Getenv("POCKETID_TOKEN")),
		ListenAddr:       strings.TrimSpace(os.Getenv("LISTEN_ADDR")),
	}

	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}

	if cfg.TelegramBotToken == "" {
		return cfg, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	if cfg.PocketIDURL == "" {
		return cfg, fmt.Errorf("POCKETID_URL is required")
	}
	if cfg.PocketIDToken == "" {
		return cfg, fmt.Errorf("POCKETID_TOKEN is required")
	}

	// validate pocketid url
	u, err := url.Parse(cfg.PocketIDURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return cfg, fmt.Errorf("POCKETID_URL must be a valid http(s) URL")
	}

	return cfg, nil
}

const INVITE_LIFETIME = 5 * 60

func onStart(ctx context.Context, b *bot.Bot, update *models.Update) {
	log.Printf("hello from %s\n", update.Message.From.Username)
	params := getUserParams(update.Message.From)

	// Always attempt to create the user first. 409 is ignored inside createPocketUser.
	if err := createPocketUser(ctx, params); err != nil {
		log.Printf("failed to create pocket user: %v", err)
		// continue to search even if create returned an error
	} else {
		log.Printf("attempted to create pocket user: %s", params.Username)
	}

	// Now search by email to obtain the user's ID (may be the existing or newly created user)
	u, err := searchPocketUser(ctx, params.Email)
	if err != nil {
		log.Printf("error searching pocket user: %v", err)
		return
	}

	if u == nil {
		log.Printf("could not find user after create: %s", params.Email)
		return
	}

	// print id to stdout
	fmt.Println(u.ID)
	log.Printf("pocket user id: %s", u.ID)

	// create one-time token
	token, err := createOneTimeToken(ctx, u.ID, INVITE_LIFETIME)
	if err != nil {
		log.Printf("failed to create one-time token: %v", err)
		return
	}

	host := strings.TrimRight(appConfig.PocketIDURL, "/")
	invite := fmt.Sprintf("%s/lc/%s", host, token)

	// send invite link back to Telegram user
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: update.Message.Chat.ID,
		Text:   fmt.Sprintf("Your single-use invite link: %s\n\nUse it to set a passkey and log in without telegram. The link will remain valid for 5 minutes.", invite),
	}); err != nil {
		log.Printf("failed to send invite link: %v", err)
	}
}

func createPocketUser(ctx context.Context, data UserCreateData) error {
	root := strings.TrimSpace(appConfig.PocketIDURL)
	token := strings.TrimSpace(appConfig.PocketIDToken)
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
		// Log full HTTP response (status, headers, body) for debugging
		log.Printf("pocket API error response: status=%d headers=%v body=%s", resp.StatusCode, resp.Header, strings.TrimSpace(string(respBody)))

		// Treat 409 Conflict as a non-error (user already exists)
		if resp.StatusCode == http.StatusConflict {
			log.Printf("pocket user already exists (409), ignoring")
			return nil
		}
		return fmt.Errorf("non-2xx response: %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	return nil
}

// createOneTimeToken requests a one-time access token for the given user ID with the
// specified TTL (seconds). It returns the token string on success.
func createOneTimeToken(ctx context.Context, userID string, ttl int) (string, error) {
	root := strings.TrimRight(appConfig.PocketIDURL, "/")
	endpoint := fmt.Sprintf("%s/api/users/%s/one-time-access-token", root, userID)

	payload := map[string]int{"ttl": ttl}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", appConfig.PocketIDToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("one-time-token error: status=%d headers=%v body=%s", resp.StatusCode, resp.Header, strings.TrimSpace(string(respBody)))
		return "", fmt.Errorf("non-2xx response: %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	// Try direct shape: {"token":"..."}
	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", err
	}

	return tokenResp.Token, nil
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
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}
	appConfig = cfg

	opts := []bot.Option{
		bot.WithDefaultHandler(onOther),
	}

	b, err := bot.New(cfg.TelegramBotToken, opts...)
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

	addr := appConfig.ListenAddr
	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("http server failed: %v", err)
	}
}
