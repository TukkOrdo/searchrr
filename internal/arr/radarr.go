package arr

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"searchrr/internal/config"
)

type Movie struct {
	ID        int     `json:"id"`
	Title     string  `json:"title"`
	Year      int     `json:"year"`
	Overview  string  `json:"overview"`
	TmdbID    int     `json:"tmdbId"`
	TitleSlug string  `json:"titleSlug"`
	HasFile   bool    `json:"hasFile"`
	Monitored bool    `json:"monitored"`
	Images    []Image `json:"images"`
}

type Radarr struct {
	*client
	minimumAvailability string
}

func NewRadarr(c *config.Config) *Radarr {
	return &Radarr{client: newClient("Radarr", c.Radarr), minimumAvailability: c.MinimumAvailability}
}

func (r *Radarr) Search(ctx context.Context, term string) ([]Movie, error) {
	var movies []Movie
	err := r.get(ctx, "/movie/lookup", url.Values{"term": {term}}, &movies)
	return movies, err
}

func (r *Radarr) inLibrary(ctx context.Context, tmdbID int) (*Movie, error) {
	var movies []Movie
	if err := r.get(ctx, "/movie", url.Values{"tmdbId": {strconv.Itoa(tmdbID)}}, &movies); err != nil {
		return nil, err
	}
	if len(movies) == 0 {
		return nil, nil
	}
	return &movies[0], nil
}

// Library entry if present, lookup result otherwise.
func (r *Radarr) Find(ctx context.Context, tmdbID int) (*Movie, error) {
	if m, err := r.inLibrary(ctx, tmdbID); err != nil || m != nil {
		return m, err
	}
	var m Movie
	if err := r.get(ctx, "/movie/lookup/tmdb", url.Values{"tmdbId": {strconv.Itoa(tmdbID)}}, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *Radarr) Request(ctx context.Context, tmdbID int) (*Movie, error) {
	m, err := r.Find(ctx, tmdbID)
	if err != nil {
		return nil, err
	}

	if m.ID > 0 {
		path := fmt.Sprintf("/movie/%d", m.ID)
		var raw map[string]any
		if err := r.get(ctx, path, nil, &raw); err != nil {
			return nil, err
		}
		raw["monitored"] = true
		if err := r.call(ctx, http.MethodPut, path, nil, raw, nil); err != nil {
			return nil, err
		}
		return m, r.command(ctx, map[string]any{"name": "MoviesSearch", "movieIds": []int{m.ID}})
	}

	def, err := r.defaults(ctx)
	if err != nil {
		return nil, err
	}
	return m, r.call(ctx, http.MethodPost, "/movie", nil, map[string]any{
		"title":               m.Title,
		"titleSlug":           m.TitleSlug,
		"tmdbId":              m.TmdbID,
		"year":                m.Year,
		"qualityProfileId":    def.profileID,
		"rootFolderPath":      def.rootFolder,
		"minimumAvailability": r.minimumAvailability,
		"tags":                def.tags,
		"monitored":           true,
		"addOptions":          map[string]any{"searchForMovie": true},
	}, nil)
}
