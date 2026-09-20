package bot

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"searchrr/internal/arr"
	"searchrr/internal/config"
	"searchrr/internal/store"
)

const maxOptions = 25

type Bot struct {
	Cfg    *config.Config
	Radarr *arr.Radarr
	Sonarr *arr.Sonarr
	// nil when notifications are off
	Store *store.Store
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
	dm := b.Cfg.AllowDMs
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

	uid := userID(i)
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
		if reason := b.denied(i, sub.Name); reason != "" {
			b.refuse(s, i, reason)
			return
		}
		if !b.ack(s, i, discordgo.InteractionResponseDeferredChannelMessageWithSource) {
			return
		}
		switch {
		case sub.Name == "movie" && b.Radarr != nil:
			v, err = b.searchMovies(ctx, uid, term)
		case sub.Name == "tv" && b.Sonarr != nil:
			v, err = b.searchShows(ctx, uid, term)
		default:
			v = view{content: "That command is not enabled."}
		}

	case discordgo.InteractionMessageComponent:
		data := i.MessageComponentData()
		parts := strings.Split(data.CustomID, ":")
		if len(parts) < 2 {
			return
		}
		if parts[1] != uid {
			b.refuse(s, i, "This is someone else's request. Use `/request` to make your own.")
			return
		}
		if !b.ack(s, i, discordgo.InteractionResponseDeferredMessageUpdate) {
			return
		}
		v, err = b.onComponent(ctx, i, data, parts)

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

// Reason the command can't be used here, or "".
func (b *Bot) denied(i *discordgo.InteractionCreate, sub string) string {
	if i.GuildID == "" {
		if !b.Cfg.AllowDMs {
			return "Requests through private messages are disabled."
		}
		return ""
	}
	if ch := b.Cfg.MonitoredChannels; len(ch) > 0 && !slices.Contains(ch, i.ChannelID) {
		return "This command is not available in this channel."
	}
	roles := b.Cfg.MovieRoles
	if sub == "tv" {
		roles = b.Cfg.TVRoles
	}
	if len(roles) == 0 {
		return ""
	}
	if i.Member != nil && slices.ContainsFunc(i.Member.Roles, func(r string) bool { return slices.Contains(roles, r) }) {
		return ""
	}
	return "You do not have the required role to use this command, please ask the server owner to give you permission."
}

func (b *Bot) refuse(s *discordgo.Session, i *discordgo.InteractionCreate, reason string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: reason, Flags: discordgo.MessageFlagsEphemeral},
	})
	if err != nil {
		log.Printf("discord response failed: %v", err)
	}
}

func (b *Bot) ack(s *discordgo.Session, i *discordgo.InteractionCreate, kind discordgo.InteractionResponseType) bool {
	data := &discordgo.InteractionResponseData{}
	if b.Cfg.HideRequests {
		data.Flags = discordgo.MessageFlagsEphemeral
	}
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{Type: kind, Data: data})
	if err != nil {
		log.Printf("discord ack failed: %v", err)
	}
	return err == nil
}

// Custom ids are action:userID[:args].
func (b *Bot) onComponent(ctx context.Context, i *discordgo.InteractionCreate, data discordgo.MessageComponentInteractionData, parts []string) (view, error) {
	action, uid, args := parts[0], parts[1], parts[2:]
	value := ""
	if len(data.Values) > 0 {
		value = data.Values[0]
	}
	menus := keptMenus(i.Message, data.CustomID, value)
	id := 0
	if len(args) > 0 {
		id, _ = strconv.Atoi(args[0])
	}

	switch {
	case action == "msel" && b.Radarr != nil:
		id, _ = strconv.Atoi(value)
		m, err := b.Radarr.Find(ctx, id)
		if err != nil {
			return view{}, err
		}
		v := b.movieView(m, uid)
		v.rows = append(menus, v.rows...)
		return v, nil

	case action == "mreq" && len(args) == 1 && b.Radarr != nil:
		m, err := b.Radarr.Request(ctx, id)
		if err != nil {
			return view{}, err
		}
		log.Printf("%s requested movie %q (tmdb:%d)", who(i), m.Title, m.TmdbID)
		content := "**" + m.Title + "** has been requested."
		if b.Store != nil && b.Cfg.NotifyRequesters {
			b.Store.AddMovie(m.TmdbID, uid)
			content += " You will be notified when it's available."
		}
		return view{content: content, embed: movieEmbed(m)}, nil

	case action == "mnote" && len(args) == 1 && b.Radarr != nil && b.Store != nil:
		m, err := b.Radarr.Find(ctx, id)
		if err != nil {
			return view{}, err
		}
		b.Store.AddMovie(m.TmdbID, uid)
		v := b.movieView(m, uid)
		v.rows = append(menus, v.rows...)
		return v, nil

	case action == "tsel" && b.Sonarr != nil:
		id, _ = strconv.Atoi(value)
		show, err := b.Sonarr.Find(ctx, id)
		if err != nil {
			return view{}, err
		}
		v := showView(show, uid)
		v.rows = append(menus, v.rows...)
		return v, nil

	case action == "tseason" && len(args) == 1 && b.Sonarr != nil:
		show, err := b.Sonarr.Find(ctx, id)
		if err != nil {
			return view{}, err
		}
		v := b.seasonView(show, value, uid)
		v.rows = append(menus, v.rows...)
		return v, nil

	case action == "treq" && len(args) == 2 && b.Sonarr != nil:
		sel := args[1]
		show, err := b.Sonarr.Request(ctx, id, sel)
		if err != nil {
			return view{}, err
		}
		log.Printf("%s requested show %q (tvdb:%d) %s", who(i), show.Title, show.TvdbID, selectionLabel(sel))
		content := fmt.Sprintf("**%s** (%s) has been requested.", show.Title, strings.ToLower(selectionLabel(sel)))
		if b.Store != nil && b.Cfg.NotifyRequesters {
			b.subscribe(show, sel, uid)
			content += " You will be notified when it's available."
		}
		return view{content: content, embed: showEmbed(show)}, nil

	case action == "tnote" && len(args) == 2 && b.Sonarr != nil && b.Store != nil:
		sel := args[1]
		show, err := b.Sonarr.Find(ctx, id)
		if err != nil {
			return view{}, err
		}
		b.subscribe(show, sel, uid)
		v := b.seasonView(show, sel, uid)
		v.rows = append(menus, v.rows...)
		return v, nil
	}
	return view{content: "This request has expired, please start again."}, nil
}

func (b *Bot) searchMovies(ctx context.Context, uid, term string) (view, error) {
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
		return b.movieView(m, uid), nil
	}
	return view{rows: []discordgo.MessageComponent{menu("msel:"+uid, "Select a movie", options)}}, nil
}

func (b *Bot) searchShows(ctx context.Context, uid, term string) (view, error) {
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
		return showView(show, uid), nil
	}
	return view{rows: []discordgo.MessageComponent{menu("tsel:"+uid, "Select a TV show", options)}}, nil
}

func (b *Bot) movieView(m *arr.Movie, uid string) view {
	v := view{embed: movieEmbed(m)}
	switch {
	case m.HasFile:
		v.rows = buttons(disabled(uid, "Already available"))
	case m.ID > 0 && m.Monitored:
		v.rows, v.content = b.requested(uid, fmt.Sprintf("mnote:%s:%d", uid, m.TmdbID), b.Store != nil && b.Store.HasMovie(m.TmdbID, uid))
	default:
		v.rows = buttons(discordgo.Button{Label: "Request", Style: discordgo.SuccessButton, CustomID: fmt.Sprintf("mreq:%s:%d", uid, m.TmdbID)})
	}
	return v
}

func (b *Bot) seasonView(show *arr.Series, sel, uid string) view {
	v := view{embed: showEmbed(show)}
	switch show.SelectionState(sel) {
	case arr.StateAvailable:
		v.rows = buttons(disabled(uid, "Already available"))
	case arr.StateRequested:
		v.rows, v.content = b.requested(uid, fmt.Sprintf("tnote:%s:%d:%s", uid, show.TvdbID, sel), b.subscribed(show, sel, uid))
	default:
		v.rows = buttons(discordgo.Button{
			Label:    "Request " + strings.ToLower(selectionLabel(sel)),
			Style:    discordgo.SuccessButton,
			CustomID: fmt.Sprintf("treq:%s:%d:%s", uid, show.TvdbID, sel),
		})
	}
	return v
}

func (b *Bot) requested(uid, notifyID string, subscribed bool) ([]discordgo.MessageComponent, string) {
	row := []discordgo.MessageComponent{disabled(uid, "Already requested")}
	switch {
	case b.Store == nil:
		return buttons(row...), ""
	case subscribed:
		return buttons(row...), "You will be notified when it's available."
	}
	row = append(row, discordgo.Button{
		Label:    "Notify me",
		Style:    discordgo.PrimaryButton,
		CustomID: notifyID,
	})
	return buttons(row...), ""
}

func showSubs(show *arr.Series, sel, uid string) []store.ShowSub {
	if sel == arr.SelFuture {
		next := 1
		for _, se := range show.Seasons {
			next = max(next, se.SeasonNumber+1)
		}
		return []store.ShowSub{{User: uid, Season: next, Future: true}}
	}
	var subs []store.ShowSub
	for _, se := range show.Seasons {
		if !arr.Wants(sel, se.SeasonNumber) || show.SeasonState(se) == arr.StateAvailable {
			continue
		}
		sub := store.ShowSub{User: uid, Season: se.SeasonNumber}
		if se.Statistics != nil {
			sub.Have = se.Statistics.EpisodeFileCount
		}
		subs = append(subs, sub)
	}
	return subs
}

func (b *Bot) subscribe(show *arr.Series, sel, uid string) {
	for _, sub := range showSubs(show, sel, uid) {
		b.Store.AddShow(show.TvdbID, sub)
	}
}

func (b *Bot) subscribed(show *arr.Series, sel, uid string) bool {
	if b.Store == nil {
		return false
	}
	for _, sub := range showSubs(show, sel, uid) {
		if !b.Store.HasShow(show.TvdbID, sub) {
			return false
		}
	}
	return true
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

func showView(show *arr.Series, uid string) view {
	seasons := show.Seasons
	if extra := len(seasons) - (maxOptions - 2); extra > 0 {
		seasons = seasons[extra:]
	}

	stateLabel := func(label string, state int) string {
		switch state {
		case arr.StateAvailable:
			return label + " (available)"
		case arr.StateRequested:
			return label + " (requested)"
		}
		return label
	}

	options := []discordgo.SelectMenuOption{{Label: stateLabel(selectionLabel(arr.SelAll), show.SelectionState(arr.SelAll)), Value: arr.SelAll}}
	if show.Status != "ended" {
		options = append(options, discordgo.SelectMenuOption{
			Label: stateLabel(selectionLabel(arr.SelFuture), show.SelectionState(arr.SelFuture)),
			Value: arr.SelFuture,
		})
	}
	for _, se := range seasons {
		n := strconv.Itoa(se.SeasonNumber)
		options = append(options, discordgo.SelectMenuOption{Label: stateLabel(selectionLabel(n), show.SeasonState(se)), Value: n})
	}

	return view{
		embed: showEmbed(show),
		rows:  []discordgo.MessageComponent{menu(fmt.Sprintf("tseason:%s:%d", uid, show.TvdbID), "Select a season", options)},
	}
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

func buttons(row ...discordgo.MessageComponent) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{discordgo.ActionsRow{Components: row}}
}

func disabled(uid, label string) discordgo.Button {
	return discordgo.Button{Label: label, Style: discordgo.SecondaryButton, CustomID: "none:" + uid, Disabled: true}
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
			if sm.CustomID == usedID {
				o.Default = o.Value == value
			}
			kept.Options = append(kept.Options, o)
		}
		rows = append(rows, discordgo.ActionsRow{Components: []discordgo.MessageComponent{kept}})
		if sm.CustomID == usedID {
			break
		}
	}
	return rows
}

func userID(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
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
