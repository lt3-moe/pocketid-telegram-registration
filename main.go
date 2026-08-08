package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"

	"github.com/lt3moe/oidc-discovery-proxy/pkg/pocketapi"
)

var pocketClient *pocketapi.Client

func getUserParams(user *models.User) pocketapi.UserCreateData {
	var username string
	if user.Username != "" {
		username = user.Username
	} else {
		username = fmt.Sprintf("id%d", user.ID)
	}

	return pocketapi.UserCreateData{
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

const INVITE_LIFETIME = 60 * 60

func onStart(ctx context.Context, b *bot.Bot, update *models.Update) {
	log.Printf("hello from %s\n", update.Message.From.Username)
	params := getUserParams(update.Message.From)

	// Always attempt to create the user first. 409 is ignored inside the API client.
	if err := pocketClient.CreateUser(ctx, params); err != nil {
		log.Printf("failed to create pocket user: %v", err)
		// continue to search even if create returned an error
	} else {
		log.Printf("attempted to create pocket user: %s", params.Username)
	}

	// Now search by email to obtain the user's ID (may be the existing or newly created user)
	u, err := pocketClient.SearchUser(ctx, params.Email)
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
	token, err := pocketClient.CreateOneTimeToken(ctx, u.ID, INVITE_LIFETIME)
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
	} else {
		log.Printf("Sent an invite link to %s (%d)", update.Message.From.Username, update.Message.From.ID)
	}
}

// API client methods moved to pkg/pocketapi

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

	// initialize API client
	pc, err := pocketapi.NewClient(appConfig.PocketIDURL, appConfig.PocketIDToken, nil)
	if err != nil {
		log.Fatalf("failed to initialize pocket api client: %v", err)
	}
	pocketClient = pc

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
