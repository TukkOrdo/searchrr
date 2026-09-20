package bot

import (
	"testing"

	"searchrr/internal/arr"
	"searchrr/internal/store"
)

func season(number, files, total int) arr.Season {
	se := arr.Season{SeasonNumber: number}
	se.Statistics = &struct {
		EpisodeFileCount  int `json:"episodeFileCount"`
		TotalEpisodeCount int `json:"totalEpisodeCount"`
	}{files, total}
	return se
}

func TestDue(t *testing.T) {
	subs := []store.ShowSub{
		{User: "fresh", Season: 2},
		{User: "partial", Season: 2, Have: 3},
		{User: "other", Season: 3},
		{User: "future", Season: 4, Future: true},
	}
	cases := []struct {
		name string
		se   arr.Season
		want []string
	}{
		{"nothing downloaded", season(2, 0, 10), nil},
		{"no new files for partial", season(2, 3, 10), []string{"fresh"}},
		{"new files", season(2, 4, 10), []string{"fresh", "partial"}},
		{"future season arrives", season(4, 1, 8), []string{"future"}},
		{"later future season", season(5, 1, 8), []string{"future"}},
		{"specials ignored", season(0, 5, 5), nil},
		{"no statistics", arr.Season{SeasonNumber: 2}, nil},
	}
	for _, c := range cases {
		var got []string
		for _, sub := range due(c.se, subs) {
			got = append(got, sub.User)
		}
		if len(got) != len(c.want) {
			t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
		}
		for n := range got {
			if got[n] != c.want[n] {
				t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
			}
		}
	}
}
