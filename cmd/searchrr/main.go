package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bwmarrin/discordgo"

	"searchrr/internal/arr"
	"searchrr/internal/bot"
	"searchrr/internal/config"
)

const version = "1.0.0"

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	log.Printf("searchrr %s", version)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	b := &bot.Bot{}
	if cfg.Radarr.URL != "" {
		b.Radarr = arr.NewRadarr(cfg)
		go check(b.Radarr)
	}
	if cfg.Sonarr.URL != "" {
		b.Sonarr = arr.NewSonarr(cfg)
		go check(b.Sonarr)
	}

	s, err := discordgo.New("Bot " + cfg.Token)
	if err != nil {
		log.Fatalf("discord: %v", err)
	}
	s.Identify.Intents = discordgo.IntentsNone
	s.AddHandler(b.OnInteraction)
	s.AddHandler(func(_ *discordgo.Session, r *discordgo.Ready) {
		log.Printf("connected to Discord as %s", r.User.Username)
	})

	if err := s.Open(); err != nil {
		log.Fatalf("discord: %v", err)
	}
	defer s.Close()

	if _, err := s.ApplicationCommandBulkOverwrite(s.State.User.ID, cfg.GuildID, b.Commands()); err != nil {
		log.Fatalf("registering commands: %v", err)
	}
	log.Print("slash commands registered")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Print("shutting down")
}

func check(c interface{ Check(context.Context) error }) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Check(ctx); err != nil {
		log.Printf("warning: %v", err)
	}
}
