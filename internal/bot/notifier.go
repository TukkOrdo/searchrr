package bot

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"time"

	"github.com/bwmarrin/discordgo"

	"searchrr/internal/arr"
	"searchrr/internal/config"
	"searchrr/internal/store"
)

const (
	pollInterval = 2 * time.Minute
	maxMessage   = 2000
)

func (b *Bot) RunNotifier(ctx context.Context, s *discordgo.Session) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.poll(ctx, s)
		}
	}
}

func (b *Bot) poll(ctx context.Context, s *discordgo.Session) {
	ctx, cancel := context.WithTimeout(ctx, pollInterval)
	defer cancel()

	if b.Radarr != nil {
		for tmdbID, users := range b.Store.Movies() {
			m, err := b.Radarr.InLibrary(ctx, tmdbID)
			switch {
			case err != nil:
				log.Printf("notifier: %v", err)
			case m == nil:
				log.Printf("notifier: tmdb:%d is gone from Radarr, dropping its notifications", tmdbID)
				b.Store.RemoveMovie(tmdbID, users)
			case m.HasFile:
				text := func(dm bool) string {
					return fmt.Sprintf("The movie **%s**%s has finished downloading!", m.Title, requestedBy(dm))
				}
				b.Store.RemoveMovie(tmdbID, b.deliver(s, users, text, movieEmbed(m)))
			}
		}
	}

	if b.Sonarr != nil {
		for tvdbID, subs := range b.Store.Shows() {
			show, err := b.Sonarr.InLibrary(ctx, tvdbID)
			switch {
			case err != nil:
				log.Printf("notifier: %v", err)
				continue
			case show == nil:
				log.Printf("notifier: tvdb:%d is gone from Sonarr, dropping its notifications", tvdbID)
				b.Store.RemoveShow(tvdbID, subs)
				continue
			}
			for _, se := range show.Seasons {
				ready := due(se, subs)
				if len(ready) == 0 {
					continue
				}
				users := make([]string, len(ready))
				for n, sub := range ready {
					users[n] = sub.User
				}
				text := func(dm bool) string {
					st := se.Statistics
					if st.EpisodeFileCount >= st.TotalEpisodeCount {
						return fmt.Sprintf("**Season %d** of **%s**%s has finished downloading!", se.SeasonNumber, show.Title, requestedBy(dm))
					}
					return fmt.Sprintf("**Season %d** of **%s**%s has started downloading, %d of %d episodes so far.",
						se.SeasonNumber, show.Title, requestedBy(dm), st.EpisodeFileCount, st.TotalEpisodeCount)
				}
				done := b.deliver(s, users, text, showEmbed(show))
				for _, sub := range ready {
					if !slices.Contains(done, sub.User) {
						continue
					}
					b.Store.RemoveShow(tvdbID, []store.ShowSub{sub})
					if sub.Future {
						b.Store.AddShow(tvdbID, store.ShowSub{User: sub.User, Season: se.SeasonNumber + 1, Future: true})
					}
				}
			}
		}
	}
}

// Subscriptions satisfied by the season's current file count.
func due(se arr.Season, subs []store.ShowSub) []store.ShowSub {
	if se.SeasonNumber == 0 || se.Statistics == nil || se.Statistics.EpisodeFileCount == 0 {
		return nil
	}
	var ready []store.ShowSub
	for _, sub := range subs {
		switch {
		case sub.Future && se.SeasonNumber >= sub.Season:
			ready = append(ready, sub)
		case !sub.Future && se.SeasonNumber == sub.Season && se.Statistics.EpisodeFileCount > sub.Have:
			ready = append(ready, sub)
		}
	}
	return ready
}

// Returns the users whose notification is settled, delivered or undeliverable.
func (b *Bot) deliver(s *discordgo.Session, users []string, text func(dm bool) string, embed *discordgo.MessageEmbed) []string {
	if b.Cfg.NotificationMode == config.NotifyChannels {
		return b.deliverToChannels(s, users, text(false), embed)
	}

	var done []string
	for _, user := range users {
		ch, err := s.UserChannelCreate(user)
		if err == nil {
			_, err = s.ChannelMessageSendComplex(ch.ID, &discordgo.MessageSend{Content: text(true), Embeds: []*discordgo.MessageEmbed{embed}})
		}
		switch {
		case err == nil:
			done = append(done, user)
		case permanent(err):
			log.Printf("notifier: cannot message user %s, dropping notification: %v", user, err)
			done = append(done, user)
		default:
			log.Printf("notifier: will retry user %s: %v", user, err)
		}
	}
	return done
}

func (b *Bot) deliverToChannels(s *discordgo.Session, users []string, text string, embed *discordgo.MessageEmbed) []string {
	type message struct {
		content string
		users   []string
	}
	messages := []message{{content: text}}
	for _, user := range users {
		mention := " <@" + user + ">"
		if last := messages[len(messages)-1]; len(last.content)+len(mention) > maxMessage {
			messages = append(messages, message{content: text})
		}
		last := &messages[len(messages)-1]
		last.content += mention
		last.users = append(last.users, user)
	}

	sent := false
	for _, channel := range b.Cfg.NotificationChannels {
		for _, m := range messages {
			_, err := s.ChannelMessageSendComplex(channel, &discordgo.MessageSend{
				Content:         m.content,
				Embeds:          []*discordgo.MessageEmbed{embed},
				AllowedMentions: &discordgo.MessageAllowedMentions{Users: m.users},
			})
			if err != nil {
				log.Printf("notifier: channel %s: %v", channel, err)
				continue
			}
			sent = true
		}
	}
	if !sent {
		return nil
	}
	return users
}

func permanent(err error) bool {
	var rest *discordgo.RESTError
	if !errors.As(err, &rest) || rest.Response == nil {
		return false
	}
	switch rest.Response.StatusCode {
	case http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound:
		return true
	}
	return false
}

func requestedBy(dm bool) string {
	if dm {
		return " that you requested"
	}
	return ""
}
