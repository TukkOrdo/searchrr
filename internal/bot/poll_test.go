package bot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/bwmarrin/discordgo"

	"searchrr/internal/arr"
	"searchrr/internal/config"
	"searchrr/internal/store"
)

type sentMessage struct {
	Channel string
	Content string `json:"content"`
}

func fakeArr(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path + "?" + r.URL.RawQuery {
		case "/api/v3/movie?tmdbId=603":
			io.WriteString(w, `[{"id":1,"title":"The Matrix","year":1999,"tmdbId":603,"hasFile":true,"monitored":true}]`)
		case "/api/v3/movie?tmdbId=604":
			io.WriteString(w, `[{"id":2,"title":"The Matrix Reloaded","tmdbId":604,"hasFile":false,"monitored":true}]`)
		case "/api/v3/movie?tmdbId=605", "/api/v3/series?tvdbId=999":
			io.WriteString(w, `[]`)
		case "/api/v3/series?tvdbId=81189":
			io.WriteString(w, `[{"id":1,"title":"Breaking Bad","tvdbId":81189,"monitored":true,"seasons":[
				{"seasonNumber":1,"monitored":true,"statistics":{"episodeFileCount":0,"totalEpisodeCount":7}},
				{"seasonNumber":2,"monitored":true,"statistics":{"episodeFileCount":13,"totalEpisodeCount":13}},
				{"seasonNumber":3,"monitored":true,"statistics":{"episodeFileCount":4,"totalEpisodeCount":13}},
				{"seasonNumber":6,"monitored":true,"statistics":{"episodeFileCount":1,"totalEpisodeCount":8}}]}]`)
		default:
			t.Errorf("unexpected arr call %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func fakeDiscord(t *testing.T) (*discordgo.Session, func() []sentMessage) {
	var mu sync.Mutex
	var sent []sentMessage

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/users/@me/channels":
			var body struct {
				RecipientID string `json:"recipient_id"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			io.WriteString(w, `{"id":"dm-`+body.RecipientID+`"}`)
		case strings.HasSuffix(r.URL.Path, "/messages"):
			channel := strings.Split(r.URL.Path, "/")[2]
			switch channel {
			case "dm-blocked":
				w.WriteHeader(http.StatusForbidden)
				io.WriteString(w, `{"code":50007,"message":"Cannot send messages to this user"}`)
				return
			case "dm-flaky":
				w.WriteHeader(http.StatusInternalServerError)
				io.WriteString(w, `{}`)
				return
			}
			msg := sentMessage{Channel: channel}
			json.NewDecoder(r.Body).Decode(&msg)
			mu.Lock()
			sent = append(sent, msg)
			mu.Unlock()
			io.WriteString(w, `{"id":"1"}`)
		default:
			t.Errorf("unexpected discord call %s", r.URL)
		}
	}))
	t.Cleanup(srv.Close)

	discordgo.EndpointUsers = srv.URL + "/users/"
	discordgo.EndpointChannels = srv.URL + "/channels/"
	s, err := discordgo.New("Bot test")
	if err != nil {
		t.Fatal(err)
	}
	return s, func() []sentMessage {
		mu.Lock()
		defer mu.Unlock()
		return append([]sentMessage(nil), sent...)
	}
}

func newTestBot(t *testing.T, mode string) *Bot {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	url := fakeArr(t).URL
	cfg := &config.Config{
		NotificationMode:     mode,
		NotificationChannels: []string{"announce"},
		Radarr:               config.Arr{URL: url, APIKey: "k"},
		Sonarr:               config.Arr{URL: url, APIKey: "k"},
	}
	return &Bot{Cfg: cfg, Radarr: arr.NewRadarr(cfg), Sonarr: arr.NewSonarr(cfg), Store: st}
}

func find(sent []sentMessage, channel string) *sentMessage {
	for _, m := range sent {
		if m.Channel == channel {
			return &m
		}
	}
	return nil
}

func TestPollDirectMessages(t *testing.T) {
	b := newTestBot(t, config.NotifyDM)
	s, sent := fakeDiscord(t)

	for _, u := range []string{"ok", "blocked", "flaky"} {
		b.Store.AddMovie(603, u)
	}
	b.Store.AddMovie(604, "ok")
	b.Store.AddMovie(605, "ok")
	b.Store.AddShow(81189, store.ShowSub{User: "ok", Season: 1})
	b.Store.AddShow(81189, store.ShowSub{User: "ok", Season: 2})
	b.Store.AddShow(81189, store.ShowSub{User: "partial", Season: 3, Have: 4})
	b.Store.AddShow(81189, store.ShowSub{User: "fan", Season: 6, Future: true})
	b.Store.AddShow(999, store.ShowSub{User: "ok", Season: 1})

	b.poll(context.Background(), s)

	movies := b.Store.Movies()
	if got := movies[603]; len(got) != 1 || got[0] != "flaky" {
		t.Errorf("603 subscribers = %v, want [flaky]", got)
	}
	if len(movies[604]) != 1 || len(movies[605]) != 0 {
		t.Errorf("604 = %v, 605 = %v", movies[604], movies[605])
	}
	if m := find(sent(), "dm-ok"); m == nil || !strings.Contains(m.Content, "**The Matrix** that you requested has finished downloading") {
		t.Errorf("movie DM = %+v", m)
	}

	shows := b.Store.Shows()
	if _, ok := shows[999]; ok {
		t.Error("removed show still has notifications")
	}
	want := map[string]int{"ok": 1, "partial": 3, "fan": 7}
	if len(shows[81189]) != len(want) {
		t.Fatalf("show subs = %+v", shows[81189])
	}
	for _, sub := range shows[81189] {
		if want[sub.User] != sub.Season || sub.Future != (sub.User == "fan") {
			t.Errorf("unexpected sub %+v", sub)
		}
	}
	if m := find(sent(), "dm-fan"); m == nil || !strings.Contains(m.Content, "**Season 6** of **Breaking Bad** that you requested has started downloading, 1 of 8") {
		t.Errorf("future DM = %+v", m)
	}
	if find(sent(), "dm-partial") != nil {
		t.Error("partial season notified without new files")
	}
	for _, m := range sent() {
		t.Logf("%s: %s", m.Channel, m.Content)
	}
}

func TestPollChannels(t *testing.T) {
	b := newTestBot(t, config.NotifyChannels)
	s, sent := fakeDiscord(t)
	b.Store.AddMovie(603, "111")
	b.Store.AddMovie(603, "222")

	b.poll(context.Background(), s)

	if len(b.Store.Movies()) != 0 {
		t.Errorf("subscribers left: %v", b.Store.Movies())
	}
	m := find(sent(), "announce")
	if m == nil || m.Content != "The movie **The Matrix** has finished downloading! <@111> <@222>" {
		t.Errorf("channel message = %+v", m)
	}
}
