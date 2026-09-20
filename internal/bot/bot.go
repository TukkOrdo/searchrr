package bot

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"searchrr/internal/arr"
)

const maxOptions = 25

type Bot struct {
	Radarr *arr.Radarr
	Sonarr *arr.Sonarr
}

type view struct {
	content string
	embed   *discordgo.MessageEmbed
	rows    []discordgo.MessageComponent
}

func (b *Bot) Commands() []*discordgo.ApplicationCommand {
	title := []*discordgo.ApplicationCommandOption{{
		Type:        discordgo.ApplicationCommandOptionString,
		Name:        "title",
		Description: "Title to search for",
		Required:    true,
	}}
	var subs []*discordgo.ApplicationCommandOption
	if b.Radarr != nil {
		subs = append(subs, &discordgo.ApplicationCommandOption{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "movie",
			Description: "Request a movie",
			Options:     title,
		})
	}
	if b.Sonarr != nil {
		subs = append(subs, &discordgo.ApplicationCommandOption{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "tv",
			Description: "Request a TV show",
			Options:     title,
		})
	}
	dm := false
	return []*discordgo.ApplicationCommand{{
		Name:         "request",
		Description:  "Request a movie or TV show",
		DMPermission: &dm,
		Options:      subs,
	}}
}

func (b *Bot) OnInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var v view
	var err error

	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		data := i.ApplicationCommandData()
		if data.Name != "request" || len(data.Options) == 0 || len(data.Options[0].Options) == 0 {
			return
		}
		sub := data.Options[0]
		term := strings.TrimSpace(sub.Options[0].StringValue())
		if !b.ack(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource) {
			return
		}
		switch {
		case sub.Name == "movie" && b.Radarr != nil:
			v, err = b.searchMovies(ctx, term)
		case sub.Name == "tv" && b.Sonarr != nil:
			v, err = b.searchShows(ctx, term)
		default:
			v = view{content: "That command is not enabled."}
		}

	case discordgo.InteractionMessageComponent:
		data := i.MessageComponentData()
		if !b.ack(s, i, discordgo.InteractionResponseDeferredMessageUpdate) {
			return
		}
		v, err = b.onComponent(ctx, i, data)

	default:
		return
	}

	if err != nil {
		log.Printf("error: %v", err)
		v = view{content: "Something went wrong: " + truncate(err.Error(), 300)}
	}

	embeds := []*discordgo.MessageEmbed{}
	if v.embed != nil {
		embeds = append(embeds, v.embed)
	}
	if v.rows == nil {
		v.rows = []discordgo.MessageComponent{}
	}
	if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
		Content:    &v.content,
		Embeds:     &embeds,
		Components: &v.rows,
	}); err != nil {
		log.Printf("discord response failed: %v", err)
	}
}

func (b *Bot) ack(s *discordgo.Session, i *discordgo.InteractionCreate, kind discordgo.InteractionResponseType) bool {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: kind,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})
	if err != nil {
		log.Printf("discord ack failed: %v", err)
	}
	return err == nil
}

func (b *Bot) onComponent(ctx context.Context, i *discordgo.InteractionCreate, data discordgo.MessageComponentInteractionData) (view, error) {
	parts := strings.Split(data.CustomID, ":")
	value := ""
	if len(data.Values) > 0 {
		value = data.Values[0]
	}
	menus := keptMenus(i.Message, data.CustomID, value)

	switch {
	case data.CustomID == "m:sel" && b.Radarr != nil:
		id, _ := strconv.Atoi(value)
		m, err := b.Radarr.Find(ctx, id)
		if err != nil {
			return view{}, err
		}
		v := movieView(m)
		v.rows = append(menus, v.rows...)
		return v, nil

	case len(parts) == 3 && parts[0] == "m" && parts[1] == "req" && b.Radarr != nil:
		id, _ := strconv.Atoi(parts[2])
		m, err := b.Radarr.Request(ctx, id)
		if err != nil {
			return view{}, err
		}
		log.Printf("%s requested movie %q (tmdb:%d)", who(i), m.Title, m.TmdbID)
		return view{content: "✅ **" + m.Title + "** has been requested.", embed: movieEmbed(m)}, nil

	case data.CustomID == "t:sel" && b.Sonarr != nil:
		id, _ := strconv.Atoi(value)
		show, err := b.Sonarr.Find(ctx, id)
		if err != nil {
			return view{}, err
		}
		v := showView(show)
		v.rows = append(menus, v.rows...)
		return v, nil

	case len(parts) == 3 && parts[0] == "t" && parts[1] == "season" && b.Sonarr != nil:
		id, _ := strconv.Atoi(parts[2])
		show, err := b.Sonarr.Find(ctx, id)
		if err != nil {
			return view{}, err
		}
		return view{embed: showEmbed(show), rows: append(menus, seasonButton(show, value))}, nil

	case len(parts) == 4 && parts[0] == "t" && parts[1] == "req" && b.Sonarr != nil:
		id, _ := strconv.Atoi(parts[2])
		show, err := b.Sonarr.Request(ctx, id, parts[3])
		if err != nil {
			return view{}, err
		}
		log.Printf("%s requested show %q (tvdb:%d) %s", who(i), show.Title, show.TvdbID, selectionLabel(parts[3]))
		return view{
			content: fmt.Sprintf("✅ **%s** (%s) has been requested.", show.Title, strings.ToLower(selectionLabel(parts[3]))),
			embed:   showEmbed(show),
		}, nil
	}
	return view{content: "This request has expired, please start again."}, nil
}

func (b *Bot) searchMovies(ctx context.Context, term string) (view, error) {
	movies, err := b.Radarr.Search(ctx, term)
	if err != nil {
		return view{}, err
	}
	var options []discordgo.SelectMenuOption
	seen := map[int]bool{}
	for _, m := range movies {
		if m.TmdbID == 0 || seen[m.TmdbID] || len(options) == maxOptions {
			continue
		}
		seen[m.TmdbID] = true
		options = append(options, discordgo.SelectMenuOption{
			Label:       truncate(withYear(m.Title, m.Year), 100),
			Value:       strconv.Itoa(m.TmdbID),
			Description: truncate(m.Overview, 100),
		})
	}
	switch len(options) {
	case 0:
		return view{content: "No movie found for `" + term + "`."}, nil
	case 1:
		id, _ := strconv.Atoi(options[0].Value)
		m, err := b.Radarr.Find(ctx, id)
		if err != nil {
			return view{}, err
		}
		return movieView(m), nil
	}
	return view{rows: []discordgo.MessageComponent{menu("m:sel", "Select a movie", options)}}, nil
}

func (b *Bot) searchShows(ctx context.Context, term string) (view, error) {
	shows, err := b.Sonarr.Search(ctx, term)
	if err != nil {
		return view{}, err
	}
	var options []discordgo.SelectMenuOption
	seen := map[int]bool{}
	for _, sh := range shows {
		if sh.TvdbID == 0 || seen[sh.TvdbID] || len(options) == maxOptions {
			continue
		}
		seen[sh.TvdbID] = true
		options = append(options, discordgo.SelectMenuOption{
			Label:       truncate(withYear(sh.Title, sh.Year), 100),
			Value:       strconv.Itoa(sh.TvdbID),
			Description: truncate(sh.Overview, 100),
		})
	}
	switch len(options) {
	case 0:
		return view{content: "No TV show found for `" + term + "`."}, nil
	case 1:
		id, _ := strconv.Atoi(options[0].Value)
		show, err := b.Sonarr.Find(ctx, id)
		if err != nil {
			return view{}, err
		}
		return showView(show), nil
	}
	return view{rows: []discordgo.MessageComponent{menu("t:sel", "Select a TV show", options)}}, nil
}

func movieView(m *arr.Movie) view {
	btn := discordgo.Button{Label: "Request", Style: discordgo.SuccessButton, CustomID: fmt.Sprintf("m:req:%d", m.TmdbID)}
	switch {
	case m.HasFile:
		btn = discordgo.Button{Label: "Already available", Style: discordgo.SecondaryButton, CustomID: "m:none", Disabled: true}
	case m.ID > 0 && m.Monitored:
		btn = discordgo.Button{Label: "Already requested", Style: discordgo.SecondaryButton, CustomID: "m:none", Disabled: true}
	}
	return view{
		embed: movieEmbed(m),
		rows:  []discordgo.MessageComponent{discordgo.ActionsRow{Components: []discordgo.MessageComponent{btn}}},
	}
}

func movieEmbed(m *arr.Movie) *discordgo.MessageEmbed {
	e := &discordgo.MessageEmbed{
		Title:       truncate(withYear(m.Title, m.Year), 256),
		URL:         fmt.Sprintf("https://www.themoviedb.org/movie/%d", m.TmdbID),
		Description: truncate(m.Overview, 400),
	}
	if p := arr.Poster(m.Images); p != "" {
		e.Image = &discordgo.MessageEmbedImage{URL: p}
	}
	return e
}

func showView(show *arr.Series) view {
	seasons := show.Seasons
	if extra := len(seasons) - (maxOptions - 2); extra > 0 {
		seasons = seasons[extra:]
	}

	var options []discordgo.SelectMenuOption
	if show.SelectionState(arr.SelAll) == arr.StateNone {
		options = append(options, discordgo.SelectMenuOption{Label: selectionLabel(arr.SelAll), Value: arr.SelAll})
	}
	if show.Status != "ended" && show.SelectionState(arr.SelFuture) == arr.StateNone {
		options = append(options, discordgo.SelectMenuOption{Label: selectionLabel(arr.SelFuture), Value: arr.SelFuture})
	}
	for _, se := range seasons {
		label := selectionLabel(strconv.Itoa(se.SeasonNumber))
		switch show.SeasonState(se) {
		case arr.StateAvailable:
			label += " (available)"
		case arr.StateRequested:
			label += " (requested)"
		}
		options = append(options, discordgo.SelectMenuOption{Label: label, Value: strconv.Itoa(se.SeasonNumber)})
	}
	if len(options) == 0 {
		options = append(options, discordgo.SelectMenuOption{Label: selectionLabel(arr.SelAll), Value: arr.SelAll})
	}

	return view{
		embed: showEmbed(show),
		rows:  []discordgo.MessageComponent{menu(fmt.Sprintf("t:season:%d", show.TvdbID), "Select a season", options)},
	}
}

func seasonButton(show *arr.Series, sel string) discordgo.MessageComponent {
	btn := discordgo.Button{
		Label:    "Request " + strings.ToLower(selectionLabel(sel)),
		Style:    discordgo.SuccessButton,
		CustomID: fmt.Sprintf("t:req:%d:%s", show.TvdbID, sel),
	}
	switch show.SelectionState(sel) {
	case arr.StateAvailable:
		btn = discordgo.Button{Label: "Already available", Style: discordgo.SecondaryButton, CustomID: "t:none", Disabled: true}
	case arr.StateRequested:
		btn = discordgo.Button{Label: "Already requested", Style: discordgo.SecondaryButton, CustomID: "t:none", Disabled: true}
	}
	return discordgo.ActionsRow{Components: []discordgo.MessageComponent{btn}}
}

func showEmbed(show *arr.Series) *discordgo.MessageEmbed {
	e := &discordgo.MessageEmbed{
		Title:       truncate(withYear(show.Title, show.Year), 256),
		URL:         fmt.Sprintf("https://www.thetvdb.com/?id=%d&tab=series", show.TvdbID),
		Description: truncate(show.Overview, 400),
	}
	if show.Network != "" {
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: "Network", Value: show.Network, Inline: true})
	}
	if show.Status != "" {
		e.Fields = append(e.Fields, &discordgo.MessageEmbedField{Name: "Status", Value: strings.ToUpper(show.Status[:1]) + show.Status[1:], Inline: true})
	}
	if p := arr.Poster(show.Images); p != "" {
		e.Image = &discordgo.MessageEmbedImage{URL: p}
	}
	return e
}

func selectionLabel(sel string) string {
	switch sel {
	case arr.SelAll:
		return "All seasons"
	case arr.SelFuture:
		return "Future seasons"
	case "0":
		return "Specials"
	}
	return "Season " + sel
}

func menu(id, placeholder string, options []discordgo.SelectMenuOption) discordgo.MessageComponent {
	return discordgo.ActionsRow{Components: []discordgo.MessageComponent{
		discordgo.SelectMenu{CustomID: id, Placeholder: placeholder, Options: options},
	}}
}

// Select menus of the message up to and including the one just used, with its choice marked.
func keptMenus(msg *discordgo.Message, usedID, value string) []discordgo.MessageComponent {
	var rows []discordgo.MessageComponent
	if msg == nil {
		return rows
	}
	for _, c := range msg.Components {
		row, ok := c.(*discordgo.ActionsRow)
		if !ok || len(row.Components) == 0 {
			break
		}
		sm, ok := row.Components[0].(*discordgo.SelectMenu)
		if !ok {
			break
		}
		kept := discordgo.SelectMenu{CustomID: sm.CustomID, Placeholder: sm.Placeholder}
		for _, o := range sm.Options {
			o.Default = sm.CustomID == usedID && o.Value == value || sm.CustomID != usedID && o.Default
			kept.Options = append(kept.Options, o)
		}
		rows = append(rows, discordgo.ActionsRow{Components: []discordgo.MessageComponent{kept}})
		if sm.CustomID == usedID {
			break
		}
	}
	return rows
}

func who(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.Username
	}
	if i.User != nil {
		return i.User.Username
	}
	return "unknown"
}

func withYear(title string, year int) string {
	if year > 0 {
		return fmt.Sprintf("%s (%d)", title, year)
	}
	return title
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
