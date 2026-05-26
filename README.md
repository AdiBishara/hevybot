# hevybot

hevybot is a Go project for automating interaction and data synchronization with the Hevy fitness platform, providing both a Telegram bot interface and historical data sync via the Hevy API. The project is Docker-ready.

## Features

- **Telegram Bot**: Exposes endpoints to interact with users through Telegram, enabling users to view workout stats, last workout summaries, and personal records via chat commands.
- **Hevy API Integration**: Syncs and fetches workout data from Hevy, supporting both real-time webhooks and paginated historical data import.
- **Database Support**: Utilizes Turso as its backend database and runs automatic migrations.
- **AI Integration**: Connects with Gemini AI for advanced bot responses.
- **Webhooks**: Implements endpoints for receiving webhook events from both Hevy and Telegram.
- **Production Ready**: Comes with health checks, structured logging (JSON or human-readable), and graceful shutdown handling.
- **Docker Support**: Ready to deploy in container environments.

## Project Structure

```
cmd/
  bot/    # Main entry for the HTTP+Telegram bot server
    main.go
  sync/   # Main entry for the historical data sync tool
    main.go
internal/
  ai/
  config/
  db/
  handlers/
  models/
  telegram/
```

## Usage

### Environment Setup

Copy `.env.example` to `.env` and provide the required secrets/keys:

- Telegram Bot Token
- Hevy API Key
- Gemini API Key and Model
- Turso DB URL and Auth Token
- Webhook Secrets

### Running the Bot Server
```sh
go run ./cmd/bot
# or with Docker:
docker build -t hevybot .
docker run --env-file .env -p 8080:8080 hevybot
```

### Running the Historical Sync
```sh
go run ./cmd/sync
```

## Endpoints

- `GET /health` — Health check endpoint.
- `POST /webhooks/hevy` — Receives workout events from Hevy.
- `POST /webhooks/telegram` — Receives updates from Telegram.

## Telegram Commands

- `/start` — Start bot and sync history.
- `/stats` — View all-time workout stats.
- `/lastworkout` — View details of your latest workout.
- `/musclegroup` — Select a muscle to see 1RM records.

## Development

- Go 1.20+ required.
- Configuration and dependencies loaded using Go Modules.
- Logging format configurable via `LOG_FORMAT` environment variable.

---

_This project is under development. Contributions and suggestions are welcome!_
