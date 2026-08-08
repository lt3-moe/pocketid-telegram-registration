package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

func onStart(ctx context.Context, bot *bot.Bot, update *models.Update) {
	log.Printf("hello from %s\n", update.Message.From.Username)
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
