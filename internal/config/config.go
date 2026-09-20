package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
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
		Radarr:              loadArr("RADARR"),
		MinimumAvailability: env("RADARR_MINIMUM_AVAILABILITY", "released"),
		Sonarr:              loadArr("SONARR"),
		SeriesType:          env("SONARR_SERIES_TYPE", "standard"),
	}

	folders, err := strconv.ParseBool(env("SONARR_SEASON_FOLDERS", "true"))
	if err != nil {
		return nil, errors.New("SONARR_SEASON_FOLDERS must be true or false")
	}
	c.SeasonFolders = folders

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
	return c, nil
}

func loadArr(prefix string) Arr {
	a := Arr{
		URL:            env(prefix+"_URL", ""),
		APIKey:         env(prefix+"_API_KEY", ""),
		QualityProfile: env(prefix+"_QUALITY_PROFILE", ""),
		RootFolder:     env(prefix+"_ROOT_FOLDER", ""),
	}
	for _, t := range strings.Split(env(prefix+"_TAGS", ""), ",") {
		if t = strings.TrimSpace(t); t != "" {
			a.Tags = append(a.Tags, t)
		}
	}
	return a
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
