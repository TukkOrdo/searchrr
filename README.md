# searchrr

Request movies and TV shows from Discord. Talks to either Radarr and/or Sonarr if configured for movies/TV.

Requires Radarr v3+ and/or Sonarr v4+.

## Setup

1. Create a bot at https://discord.com/developers/applications and copy its token.
2. Invite it to your server (swap in your application id):
   `https://discord.com/oauth2/authorize?client_id=APP_ID&scope=bot+applications.commands`
3. Copy `.env.example` to `.env` and fill it in. See below for variable details.
4. Start it:

```bash
docker compose up -d --build
```

## Commands

```
/request movie <title>
/request tv <title>
```

Pick a result, pick a season if it's a show, then hit "Request". You can also search by ID: `tmdb:603` or `imdb:tt0133093` for movies, `tvdb:81189` for shows.

If something is already in Radarr or Sonarr but unmonitored, it gets monitored and searched instead of added again.

## Notifications

A notification is sent when a request has been downloaded, either by private message or in a channel where they get mentioned. If someone looks up something that was already requested, they will get a "Notify me" button, which will send a notification when the original request is complete.

Pending notifications are kept in `/data/notifications.json`, so mount `/data` somewhere (the compose file uses `./data`, on Unraid use `/mnt/user/appdata/searchrr`). Radarr and Sonarr are checked every 2 minutes. Set `NOTIFICATION_MODE=off` if you don't want notifications.

## Settings

| Variable | Default | Notes |
|---|---|---|
| `DISCORD_TOKEN` | | Required. |
| `DISCORD_GUILD_ID` | | Commands show up instantly in this server instead of globally. |
| `DISCORD_MOVIE_ROLES`, `DISCORD_TV_ROLES` | everyone | Role IDs allowed to request, comma separated. |
| `DISCORD_MONITORED_CHANNELS` | all | Channel IDs where `/request` works. |
| `DISCORD_ALLOW_DMS` | `false` | Allow requests by PM. Roles and channels aren't checked there. Makes the commands global, which can take up to an hour to show up. |
| `DISCORD_HIDE_REQUESTS` | `true` | Only the requester sees the request messages. |
| `NOTIFICATION_MODE` | `dm` | `off`, `dm` or `channels`. |
| `NOTIFICATION_CHANNELS` | | Channel IDs to post in when the mode is `channels`. |
| `NOTIFY_REQUESTERS` | `true` | Notify requesters automatically. When `false` only people who press "Notify me" get notified. |
| `RADARR_URL`, `SONARR_URL` | | Include the URL base if you use one. Leave empty to turn it off. |
| `RADARR_API_KEY`, `SONARR_API_KEY` | | |
| `RADARR_QUALITY_PROFILE`, `SONARR_QUALITY_PROFILE` | First one | Name or ID |
| `RADARR_ROOT_FOLDER`, `SONARR_ROOT_FOLDER` | First one | |
| `RADARR_TAGS`, `SONARR_TAGS` | | Comma separated, tags must already exist if populated. |
| `RADARR_MINIMUM_AVAILABILITY` | `released` | `announced`, `inCinemas` or `released`. |
| `SONARR_SERIES_TYPE` | `standard` | `standard`, `daily` or `anime`. |
| `SONARR_SEASON_FOLDERS` | `true` | |
