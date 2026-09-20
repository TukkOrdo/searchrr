package arr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"searchrr/internal/config"
)

type Image struct {
	CoverType string `json:"coverType"`
	URL       string `json:"url"`
	RemoteURL string `json:"remoteUrl"`
}

func Poster(images []Image) string {
	for _, img := range images {
		if img.CoverType != "poster" {
			continue
		}
		for _, u := range []string{img.RemoteURL, img.URL} {
			if strings.HasPrefix(u, "http") {
				return u
			}
		}
	}
	return ""
}

type defaults struct {
	profileID  int
	rootFolder string
	tags       []int
}

// Shared Sonarr/Radarr v3 API client.
type client struct {
	name string
	base string
	cfg  config.Arr
	http *http.Client

	mu  sync.Mutex
	def *defaults
}

func newClient(name string, cfg config.Arr) *client {
	return &client{
		name: name,
		base: strings.TrimRight(cfg.URL, "/") + "/api/v3",
		cfg:  cfg,
		http: &http.Client{Timeout: 60 * time.Second},
	}
}

func (a *client) call(ctx context.Context, method, path string, query url.Values, body, out any) error {
	u := a.base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", a.cfg.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := a.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s unreachable: %w", a.name, err)
	}
	defer res.Body.Close()

	if res.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("%s %s %s: %s: %s", a.name, method, path, res.Status, bytes.TrimSpace(snippet))
	}
	if out == nil {
		return nil
	}
	dec := json.NewDecoder(res.Body)
	dec.UseNumber()
	return dec.Decode(out)
}

func (a *client) get(ctx context.Context, path string, query url.Values, out any) error {
	return a.call(ctx, http.MethodGet, path, query, nil, out)
}

func (a *client) command(ctx context.Context, body map[string]any) error {
	return a.call(ctx, http.MethodPost, "/command", nil, body, nil)
}

func (a *client) Check(ctx context.Context) error {
	_, err := a.defaults(ctx)
	return err
}

// Resolves quality profile, root folder and tags once, then caches them.
func (a *client) defaults(ctx context.Context) (*defaults, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.def != nil {
		return a.def, nil
	}

	var profiles []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := a.get(ctx, "/qualityprofile", nil, &profiles); err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return nil, fmt.Errorf("%s has no quality profiles", a.name)
	}
	d := &defaults{profileID: profiles[0].ID, tags: []int{}}
	if want := a.cfg.QualityProfile; want != "" {
		found := false
		for _, p := range profiles {
			if strings.EqualFold(p.Name, want) || strconv.Itoa(p.ID) == want {
				d.profileID, found = p.ID, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%s quality profile %q not found", a.name, want)
		}
	}

	d.rootFolder = a.cfg.RootFolder
	if d.rootFolder == "" {
		var folders []struct {
			Path string `json:"path"`
		}
		if err := a.get(ctx, "/rootfolder", nil, &folders); err != nil {
			return nil, err
		}
		if len(folders) == 0 {
			return nil, fmt.Errorf("%s has no root folders", a.name)
		}
		d.rootFolder = folders[0].Path
	}

	if len(a.cfg.Tags) > 0 {
		var tags []struct {
			ID    int    `json:"id"`
			Label string `json:"label"`
		}
		if err := a.get(ctx, "/tag", nil, &tags); err != nil {
			return nil, err
		}
		for _, want := range a.cfg.Tags {
			found := false
			for _, t := range tags {
				if strings.EqualFold(t.Label, want) {
					d.tags, found = append(d.tags, t.ID), true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("%s tag %q not found", a.name, want)
			}
		}
	}

	log.Printf("%s: quality profile %d, root folder %s, tags %v", a.name, d.profileID, d.rootFolder, d.tags)
	a.def = d
	return d, nil
}
