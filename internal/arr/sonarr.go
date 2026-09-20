package arr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"searchrr/internal/config"
)

const (
	SelAll    = "all"
	SelFuture = "future"
)

type Season struct {
	SeasonNumber int  `json:"seasonNumber"`
	Monitored    bool `json:"monitored"`
	Statistics   *struct {
		EpisodeFileCount  int `json:"episodeFileCount"`
		TotalEpisodeCount int `json:"totalEpisodeCount"`
	} `json:"statistics"`
}

type Series struct {
	ID        int      `json:"id"`
	Title     string   `json:"title"`
	Year      int      `json:"year"`
	Overview  string   `json:"overview"`
	Network   string   `json:"network"`
	Status    string   `json:"status"`
	TvdbID    int      `json:"tvdbId"`
	TitleSlug string   `json:"titleSlug"`
	Monitored bool     `json:"monitored"`
	Seasons   []Season `json:"seasons"`
	Images    []Image  `json:"images"`
}

const (
	StateNone = iota
	StateRequested
	StateAvailable
)

func (s *Series) SeasonState(se Season) int {
	if s.ID == 0 {
		return StateNone
	}
	if st := se.Statistics; st != nil && st.TotalEpisodeCount > 0 && st.EpisodeFileCount >= st.TotalEpisodeCount {
		return StateAvailable
	}
	if s.Monitored && se.Monitored {
		return StateRequested
	}
	return StateNone
}

// Lowest state across the seasons covered by sel.
func (s *Series) SelectionState(sel string) int {
	if sel == SelFuture {
		if s.ID > 0 && s.Monitored {
			return StateRequested
		}
		return StateNone
	}
	state, matched := StateAvailable, false
	for _, se := range s.Seasons {
		if wants(sel, se.SeasonNumber) {
			matched = true
			state = min(state, s.SeasonState(se))
		}
	}
	if !matched {
		return StateNone
	}
	return state
}

func wants(sel string, seasonNumber int) bool {
	switch sel {
	case SelAll:
		return seasonNumber > 0
	case SelFuture:
		return false
	}
	return sel == strconv.Itoa(seasonNumber)
}

type Sonarr struct {
	*client
	seriesType    string
	seasonFolders bool
}

func NewSonarr(c *config.Config) *Sonarr {
	return &Sonarr{client: newClient("Sonarr", c.Sonarr), seriesType: c.SeriesType, seasonFolders: c.SeasonFolders}
}

func (s *Sonarr) Search(ctx context.Context, term string) ([]Series, error) {
	var found []Series
	err := s.get(ctx, "/series/lookup", url.Values{"term": {term}}, &found)
	return found, err
}

func (s *Sonarr) inLibrary(ctx context.Context, tvdbID int) (*Series, error) {
	var found []Series
	if err := s.get(ctx, "/series", url.Values{"tvdbId": {strconv.Itoa(tvdbID)}}, &found); err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, nil
	}
	return &found[0], nil
}

// Library entry if present, lookup result otherwise.
func (s *Sonarr) Find(ctx context.Context, tvdbID int) (*Series, error) {
	if se, err := s.inLibrary(ctx, tvdbID); err != nil || se != nil {
		return se, err
	}
	found, err := s.Search(ctx, "tvdb:"+strconv.Itoa(tvdbID))
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("tvdb:%d not found", tvdbID)
	}
	return &found[0], nil
}

func (s *Sonarr) Request(ctx context.Context, tvdbID int, sel string) (*Series, error) {
	show, err := s.Find(ctx, tvdbID)
	if err != nil {
		return nil, err
	}
	if show.ID > 0 {
		return show, s.update(ctx, show.ID, sel)
	}

	def, err := s.defaults(ctx)
	if err != nil {
		return nil, err
	}
	seasons := make([]map[string]any, 0, len(show.Seasons))
	for _, se := range show.Seasons {
		seasons = append(seasons, map[string]any{
			"seasonNumber": se.SeasonNumber,
			"monitored":    wants(sel, se.SeasonNumber),
		})
	}
	return show, s.call(ctx, http.MethodPost, "/series", nil, map[string]any{
		"title":            show.Title,
		"titleSlug":        show.TitleSlug,
		"tvdbId":           show.TvdbID,
		"year":             show.Year,
		"qualityProfileId": def.profileID,
		"rootFolderPath":   def.rootFolder,
		"seriesType":       s.seriesType,
		"seasonFolder":     s.seasonFolders,
		"tags":             def.tags,
		"monitored":        true,
		"seasons":          seasons,
		"addOptions":       map[string]any{"searchForMissingEpisodes": true},
	}, nil)
}

func (s *Sonarr) update(ctx context.Context, id int, sel string) error {
	path := fmt.Sprintf("/series/%d", id)
	var raw map[string]any
	if err := s.get(ctx, path, nil, &raw); err != nil {
		return err
	}
	raw["monitored"] = true

	var targets []int
	seasons, _ := raw["seasons"].([]any)
	setMonitored := func(v bool) {
		targets = targets[:0]
		for _, item := range seasons {
			se, _ := item.(map[string]any)
			num, _ := se["seasonNumber"].(json.Number)
			n, err := num.Int64()
			if err == nil && wants(sel, int(n)) {
				se["monitored"] = v
				targets = append(targets, int(n))
			}
		}
	}

	// Off then on: Sonarr cascades to episodes only on a flag change.
	if setMonitored(false); len(targets) > 0 {
		if err := s.call(ctx, http.MethodPut, path, nil, raw, nil); err != nil {
			return err
		}
		setMonitored(true)
	}
	if err := s.call(ctx, http.MethodPut, path, nil, raw, nil); err != nil {
		return err
	}

	if sel == SelAll {
		return s.command(ctx, map[string]any{"name": "SeriesSearch", "seriesId": id})
	}
	for _, n := range targets {
		if err := s.command(ctx, map[string]any{"name": "SeasonSearch", "seriesId": id, "seasonNumber": n}); err != nil {
			return err
		}
	}
	return nil
}
