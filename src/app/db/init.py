from __future__ import annotations

from app.db.base import Base
from app.db.session import engine
from app.models import *  # noqa: F403


async def init_database() -> None:
    async with engine.begin() as connection:
        await connection.run_sync(Base.metadata.create_all)

