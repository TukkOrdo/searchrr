package bot

import (
	"testing"

	"github.com/bwmarrin/discordgo"

	"searchrr/internal/config"
)

func interaction(guild, channel string, roles ...string) *discordgo.InteractionCreate {
	i := &discordgo.Interaction{GuildID: guild, ChannelID: channel}
	if guild != "" {
		i.Member = &discordgo.Member{Roles: roles, User: &discordgo.User{ID: "u"}}
	} else {
		i.User = &discordgo.User{ID: "u"}
	}
	return &discordgo.InteractionCreate{Interaction: i}
}

func TestDenied(t *testing.T) {
	open := &Bot{Cfg: &config.Config{}}
	locked := &Bot{Cfg: &config.Config{
		MovieRoles:        []string{"10", "11"},
		MonitoredChannels: []string{"c1"},
		AllowDMs:          true,
	}}
	cases := []struct {
		name    string
		bot     *Bot
		i       *discordgo.InteractionCreate
		sub     string
		allowed bool
	}{
		{"no restrictions", open, interaction("g", "c9"), "movie", true},
		{"dm while disabled", open, interaction("", "dm"), "movie", false},
		{"dm skips roles and channels", locked, interaction("", "dm"), "movie", true},
		{"wrong channel", locked, interaction("g", "c2", "10"), "movie", false},
		{"missing role", locked, interaction("g", "c1", "99"), "movie", false},
		{"has one of the roles", locked, interaction("g", "c1", "99", "11"), "movie", true},
		{"tv roles are separate", locked, interaction("g", "c1"), "tv", true},
	}
	for _, c := range cases {
		if reason := c.bot.denied(c.i, c.sub); (reason == "") != c.allowed {
			t.Errorf("%s: reason %q, allowed should be %v", c.name, reason, c.allowed)
		}
	}
}
