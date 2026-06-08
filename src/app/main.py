from __future__ import annotations

import asyncio
import logging

from aiogram import Bot, Dispatcher
from aiogram.client.default import DefaultBotProperties
from aiogram.enums import ParseMode
from aiogram.fsm.storage.memory import MemoryStorage

from app.bot.handlers import setup_routers
from app.bot.middlewares import DbSessionMiddleware
from app.core.config import settings


def create_dispatcher() -> Dispatcher:
    dp = Dispatcher(storage=MemoryStorage())
    dp.update.middleware(DbSessionMiddleware())
    dp.include_router(setup_routers())
    return dp


def create_bot() -> Bot:
    return Bot(
        token=settings.bot_token,
        default=DefaultBotProperties(parse_mode=ParseMode.HTML),
    )


async def run_bot() -> None:
    logging.basicConfig(level=settings.log_level)
    bot = create_bot()
    dp = create_dispatcher()
    await dp.start_polling(bot)


def main() -> None:
    asyncio.run(run_bot())
