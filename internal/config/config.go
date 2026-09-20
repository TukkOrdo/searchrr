package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	NotifyOff      = "off"
	NotifyDM       = "dm"
	NotifyChannels = "channels"
)

type Arr struct {
	URL            string
	APIKey         string
	QualityProfile string
	RootFolder     string
	Tags           []string
}

type Config struct {
	Token   string
	GuildID string

	MovieRoles        []string
	TVRoles           []string
	MonitoredChannels []string
	AllowDMs          bool
	HideRequests      bool

	NotificationMode     string
	NotificationChannels []string
	NotifyRequesters     bool
	DataDir              string

	Radarr              Arr
	MinimumAvailability string

	Sonarr        Arr
	SeriesType    string
	SeasonFolders bool
}

func Load() (*Config, error) {
	c := &Config{
		Token:               env("DISCORD_TOKEN", ""),
		GuildID:             env("DISCORD_GUILD_ID", ""),
		NotificationMode:    strings.ToLower(env("NOTIFICATION_MODE", NotifyDM)),
		DataDir:             env("DATA_DIR", "/data"),
		Radarr:              loadArr("RADARR"),
		MinimumAvailability: env("RADARR_MINIMUM_AVAILABILITY", "released"),
		Sonarr:              loadArr("SONARR"),
		SeriesType:          env("SONARR_SERIES_TYPE", "standard"),
	}

	var err error
	for _, id := range []struct {
		key string
		dst *[]string
	}{
		{"DISCORD_MOVIE_ROLES", &c.MovieRoles},
		{"DISCORD_TV_ROLES", &c.TVRoles},
		{"DISCORD_MONITORED_CHANNELS", &c.MonitoredChannels},
		{"NOTIFICATION_CHANNELS", &c.NotificationChannels},
	} {
		if *id.dst, err = ids(id.key); err != nil {
			return nil, err
		}
	}
	for _, b := range []struct {
		key      string
		fallback string
		dst      *bool
	}{
		{"DISCORD_ALLOW_DMS", "false", &c.AllowDMs},
		{"DISCORD_HIDE_REQUESTS", "true", &c.HideRequests},
		{"NOTIFY_REQUESTERS", "true", &c.NotifyRequesters},
		{"SONARR_SEASON_FOLDERS", "true", &c.SeasonFolders},
	} {
		if *b.dst, err = strconv.ParseBool(env(b.key, b.fallback)); err != nil {
			return nil, fmt.Errorf("%s must be true or false", b.key)
		}
	}

	if c.Token == "" {
		return nil, errors.New("DISCORD_TOKEN is required")
	}
	if c.Radarr.URL == "" && c.Sonarr.URL == "" {
		return nil, errors.New("set RADARR_URL and/or SONARR_URL")
	}
	for name, a := range map[string]Arr{"RADARR": c.Radarr, "SONARR": c.Sonarr} {
		if a.URL != "" && a.APIKey == "" {
			return nil, errors.New(name + "_API_KEY is required")
		}
	}
	switch c.NotificationMode {
	case NotifyOff, NotifyDM:
	case NotifyChannels:
		if len(c.NotificationChannels) == 0 {
			return nil, errors.New("NOTIFICATION_MODE=channels needs NOTIFICATION_CHANNELS")
		}
	default:
		return nil, errors.New("NOTIFICATION_MODE must be off, dm or channels")
	}
	return c, nil
}

func loadArr(prefix string) Arr {
	return Arr{
		URL:            env(prefix+"_URL", ""),
		APIKey:         env(prefix+"_API_KEY", ""),
		QualityProfile: env(prefix+"_QUALITY_PROFILE", ""),
		RootFolder:     env(prefix+"_ROOT_FOLDER", ""),
		Tags:           list(prefix + "_TAGS"),
	}
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func list(key string) []string {
	var out []string
	for _, v := range strings.Split(env(key, ""), ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func ids(key string) ([]string, error) {
	out := list(key)
	for _, v := range out {
		if _, err := strconv.ParseUint(v, 10, 64); err != nil {
			return nil, fmt.Errorf("%s: %q is not a Discord id", key, v)
		}
	}
	return out, nil
}
