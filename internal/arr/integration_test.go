//go:build integration

package arr

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"searchrr/internal/config"
)

// Run against disposable Radarr/Sonarr instances: go test -tags integration

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestRadarr(t *testing.T) {
	cfg := testConfig(t)
	if cfg.Radarr.URL == "" {
		t.Skip("RADARR_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	r := NewRadarr(cfg)
	_ = r.call(ctx, http.MethodPost, "/rootfolder", nil, map[string]any{"path": "/movies"}, nil)

	found, err := r.Search(ctx, "the matrix")
	if err != nil || len(found) == 0 {
		t.Fatalf("search: %v (%d results)", err, len(found))
	}
	t.Logf("first result: %s (%d) tmdb:%d poster:%s", found[0].Title, found[0].Year, found[0].TmdbID, Poster(found[0].Images))

	const tmdbID = 603
	m, err := r.Find(ctx, tmdbID)
	if err != nil || m.ID != 0 || m.Title == "" {
		t.Fatalf("find before request: %+v %v", m, err)
	}
	if _, err := r.Request(ctx, tmdbID); err != nil {
		t.Fatalf("request: %v", err)
	}
	m, err = r.Find(ctx, tmdbID)
	if err != nil || m.ID == 0 || !m.Monitored {
		t.Fatalf("find after request: %+v %v", m, err)
	}

	// Unmonitor, then request again through the existing-movie path.
	var raw map[string]any
	path := "/movie/" + strconv.Itoa(m.ID)
	if err := r.get(ctx, path, nil, &raw); err != nil {
		t.Fatal(err)
	}
	raw["monitored"] = false
	if err := r.call(ctx, http.MethodPut, path, nil, raw, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Request(ctx, tmdbID); err != nil {
		t.Fatalf("re-request: %v", err)
	}
	if m, _ = r.Find(ctx, tmdbID); m == nil || !m.Monitored {
		t.Fatalf("not monitored after re-request: %+v", m)
	}
}

func TestSonarr(t *testing.T) {
	cfg := testConfig(t)
	if cfg.Sonarr.URL == "" {
		t.Skip("SONARR_URL not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	s := NewSonarr(cfg)
	_ = s.call(ctx, http.MethodPost, "/rootfolder", nil, map[string]any{"path": "/tv"}, nil)

	found, err := s.Search(ctx, "breaking bad")
	if err != nil || len(found) == 0 {
		t.Fatalf("search: %v (%d results)", err, len(found))
	}
	t.Logf("first result: %s (%d) tvdb:%d seasons:%d", found[0].Title, found[0].Year, found[0].TvdbID, len(found[0].Seasons))

	const tvdbID = 81189
	if _, err := s.Request(ctx, tvdbID, "2"); err != nil {
		t.Fatalf("request season 2: %v", err)
	}
	waitForSeasons(t, ctx, s, tvdbID)
	show := mustFind(t, ctx, s, tvdbID)
	if got := monitoredSeasons(show); len(got) != 1 || got[0] != 2 {
		t.Fatalf("monitored seasons after add = %v, want [2]", got)
	}
	if show.SelectionState("2") != StateRequested || show.SelectionState("3") != StateNone || show.SelectionState(SelAll) != StateNone {
		t.Fatalf("unexpected states: s2=%d s3=%d all=%d", show.SelectionState("2"), show.SelectionState("3"), show.SelectionState(SelAll))
	}

	if _, err := s.Request(ctx, tvdbID, "3"); err != nil {
		t.Fatalf("request season 3: %v", err)
	}
	if got := monitoredSeasons(mustFind(t, ctx, s, tvdbID)); len(got) != 2 {
		t.Fatalf("monitored seasons after update = %v, want [2 3]", got)
	}

	var episodes []struct {
		SeasonNumber int  `json:"seasonNumber"`
		Monitored    bool `json:"monitored"`
	}
	if err := s.get(ctx, "/episode", map[string][]string{"seriesId": {strconv.Itoa(show.ID)}}, &episodes); err != nil {
		t.Fatal(err)
	}
	for _, e := range episodes {
		if want := e.SeasonNumber == 2 || e.SeasonNumber == 3; e.Monitored != want {
			t.Fatalf("S%d episode monitored=%v, want %v", e.SeasonNumber, e.Monitored, want)
		}
	}
	t.Logf("checked %d episodes", len(episodes))

	if _, err := s.Request(ctx, tvdbID, SelAll); err != nil {
		t.Fatalf("request all: %v", err)
	}
	if st := mustFind(t, ctx, s, tvdbID).SelectionState(SelAll); st != StateRequested {
		t.Fatalf("all seasons state = %d, want requested", st)
	}
}

func mustFind(t *testing.T, ctx context.Context, s *Sonarr, tvdbID int) *Series {
	t.Helper()
	show, err := s.Find(ctx, tvdbID)
	if err != nil || show.ID == 0 {
		t.Fatalf("find: %+v %v", show, err)
	}
	return show
}

// Sonarr populates episodes asynchronously after an add.
func waitForSeasons(t *testing.T, ctx context.Context, s *Sonarr, tvdbID int) {
	t.Helper()
	for range 60 {
		show := mustFind(t, ctx, s, tvdbID)
		for _, se := range show.Seasons {
			if se.Statistics != nil && se.Statistics.TotalEpisodeCount > 0 {
				return
			}
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatal("episodes never appeared")
}

func monitoredSeasons(show *Series) []int {
	var out []int
	for _, se := range show.Seasons {
		if se.Monitored {
			out = append(out, se.SeasonNumber)
		}
	}
	return out
}
