from __future__ import annotations

from datetime import datetime, timezone

from sqlalchemy import select
from sqlalchemy.ext.asyncio import AsyncSession

from app.models import Gender, PreferredGender, User, UserStatus


async def get_by_telegram_id(session: AsyncSession, telegram_id: int) -> User | None:
    result = await session.execute(select(User).where(User.telegram_id == telegram_id))
    return result.scalar_one_or_none()


async def get_or_create_user(session: AsyncSession, telegram_id: int, username: str) -> User:
    user = await get_by_telegram_id(session, telegram_id)
    if user is not None:
        user.username = username
        return user

    user = User(telegram_id=telegram_id, username=username)
    session.add(user)
    await session.flush()
    return user


async def update_profile(
    session: AsyncSession,
    user: User,
    *,
    name: str | None = None,
    gender: Gender | None = None,
    preferred_gender: PreferredGender | None = None,
) -> User:
    if name is not None:
        user.name = name
    if gender is not None:
        user.gender = gender
    if preferred_gender is not None:
        user.preferred_gender = preferred_gender
    await session.flush()
    return user


def is_registered(user: User | None) -> bool:
    return bool(user and user.name and user.gender and user.preferred_gender)


def is_restricted(user: User) -> bool:
    if user.status == UserStatus.blocked:
        return True
    if user.restricted_until is None:
        return False
    return user.restricted_until > datetime.now(timezone.utc)

