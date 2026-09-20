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
	"searchrr/internal/store"
)

const version = "1.1.2"

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)
	log.Printf("searchrr %s", version)

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	b := &bot.Bot{Cfg: cfg}
	if cfg.Radarr.URL != "" {
		b.Radarr = arr.NewRadarr(cfg)
		go check(b.Radarr)
	}
	if cfg.Sonarr.URL != "" {
		b.Sonarr = arr.NewSonarr(cfg)
		go check(b.Sonarr)
	}
	if cfg.NotificationMode != config.NotifyOff {
		if b.Store, err = store.Open(cfg.DataDir); err != nil {
			log.Fatalf("notifications: %v (mount a writable folder at %s or set NOTIFICATION_MODE=off)", err, cfg.DataDir)
		}
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
	registerCommands(s, cfg, b.Commands())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if b.Store != nil {
		go b.RunNotifier(ctx, s)
	}
	<-ctx.Done()
	log.Print("shutting down")
}

func registerCommands(s *discordgo.Session, cfg *config.Config, cmds []*discordgo.ApplicationCommand) {
	app := s.State.User.ID
	scope := cfg.GuildID
	if cfg.AllowDMs {
		scope = ""
	}
	if _, err := s.ApplicationCommandBulkOverwrite(app, scope, cmds); err != nil {
		log.Fatalf("registering commands: %v", err)
	}
	if scope == "" {
		log.Print("slash commands registered globally, new commands can take up to an hour to show up")
	} else {
		log.Printf("slash commands registered for server %s", scope)
	}

	if cfg.GuildID == "" {
		return
	}
	other := ""
	if scope == "" {
		other = cfg.GuildID
	}
	if _, err := s.ApplicationCommandBulkOverwrite(app, other, []*discordgo.ApplicationCommand{}); err != nil {
		log.Printf("warning: clearing duplicate commands: %v", err)
	}
}

func check(c interface{ Check(context.Context) error }) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := c.Check(ctx); err != nil {
		log.Printf("warning: %v", err)
	}
}
