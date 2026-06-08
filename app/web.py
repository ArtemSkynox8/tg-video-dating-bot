from __future__ import annotations

import asyncio
import logging
import os

from fastapi import FastAPI
import uvicorn

from app.core.config import settings
from app.main import create_bot, create_dispatcher

app = FastAPI(title="Telegram video dating bot")

_bot_task: asyncio.Task | None = None


@app.on_event("startup")
async def startup() -> None:
    global _bot_task
    logging.basicConfig(level=settings.log_level)
    bot = create_bot()
    dispatcher = create_dispatcher()
    _bot_task = asyncio.create_task(dispatcher.start_polling(bot))


@app.on_event("shutdown")
async def shutdown() -> None:
    if _bot_task is not None:
        _bot_task.cancel()


@app.get("/health")
async def health() -> dict[str, str]:
    return {"status": "ok"}


def main() -> None:
    port = int(os.getenv("PORT", "8000"))
    uvicorn.run("app.web:app", host="0.0.0.0", port=port)


if __name__ == "__main__":
    main()
