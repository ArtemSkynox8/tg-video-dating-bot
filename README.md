# MAX Video Dating Bot

Dating bot for MAX messenger where users meet through short videos.

## Stack

- Go
- MAX Bot API over HTTPS
- PostgreSQL
- Redis-ready architecture
- Docker Compose

## Local Run

1. Copy `.env.example` to `.env`.
2. Fill `MAX_BOT_TOKEN`, `MAX_WEBHOOK_SECRET`, and `PUBLIC_BASE_URL`.
3. Start PostgreSQL and Redis:

```bash
docker compose up -d postgres redis
```

4. Run migrations:

```bash
docker compose --profile tools run --rm migrate
```

5. Start the bot:

```bash
docker compose up --build
```

Webhook endpoint:

```text
POST /webhook/max
```

Healthcheck:

```text
GET /healthz
```

The code intentionally uses platform-neutral names: `platform_user_id`,
`platform_media_id`, `chat_id`, and `message_id`, so another messenger adapter can
be added later.
