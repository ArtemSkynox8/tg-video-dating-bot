# Telegram bot dating via video notes

Async Telegram bot for dating through Telegram video notes ("circles").

## Stack

- Python 3.11+
- aiogram 3
- PostgreSQL
- SQLAlchemy asyncio
- Alembic

## Run locally

1. Create `.env` from `.env.example`.
2. Install dependencies:

```bash
pip install --upgrade -r requirements.txt
```

3. Apply migrations:

```bash
alembic upgrade head
```

4. Start the bot:

```bash
python -m app
```

For platforms that require an HTTP healthcheck, run:

```bash
uvicorn app.web:app --host 0.0.0.0 --port 8000
```

## Implemented base

- Registration FSM: username check, name, gender, preferred gender, video note.
- Main menu.
- Browsing active video notes with gender filters and no repeat views.
- Like, next, report actions.
- Mutual likes and hidden contacts.
- Report limits and automatic temporary restrictions.
- Profile edit and video note replacement.
- Minimal admin commands.
- Premium placeholders for later payments.
