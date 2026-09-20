# searchrr

Request movies and TV shows from Discord. It talks to Radarr and Sonarr and that's all it does.

Requires Radarr v3+ and/or Sonarr v4+.

## Setup

1. Create a bot at https://discord.com/developers/applications and copy its token.
2. Invite it to your server (swap in your application id):
   `https://discord.com/oauth2/authorize?client_id=APP_ID&scope=bot+applications.commands`
3. Copy `.env.example` to `.env` and fill it in.
4. Start it:

```bash
docker compose up -d --build
```

## Commands

```
/request movie <title>
/request tv <title>
```

Pick a result, pick a season if it's a show, hit Request. Only the person requesting sees the messages. You can also search by id: `tmdb:603` or `imdb:tt0133093` for movies, `tvdb:81189` for shows.

If something is already in Radarr or Sonarr but unmonitored, it gets monitored and searched instead of added again.

To limit who can use the command, use Server Settings > Integrations in Discord.

## Settings

| Variable | Default | Notes |
|---|---|---|
| `DISCORD_TOKEN` | | required |
| `DISCORD_GUILD_ID` | | commands show up instantly in this server instead of globally (which can take a while) |
| `RADARR_URL`, `SONARR_URL` | | include the URL base if you use one. Leave empty to turn that side off |
| `RADARR_API_KEY`, `SONARR_API_KEY` | | |
| `RADARR_QUALITY_PROFILE`, `SONARR_QUALITY_PROFILE` | first one | name or id |
| `RADARR_ROOT_FOLDER`, `SONARR_ROOT_FOLDER` | first one | |
| `RADARR_TAGS`, `SONARR_TAGS` | | comma separated, tags must already exist |
| `RADARR_MINIMUM_AVAILABILITY` | `released` | `announced`, `inCinemas` or `released` |
| `SONARR_SERIES_TYPE` | `standard` | `standard`, `daily` or `anime` |
| `SONARR_SEASON_FOLDERS` | `true` | |