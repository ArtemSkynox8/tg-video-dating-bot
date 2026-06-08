from __future__ import annotations

from datetime import datetime, timezone

from aiogram import Router
from aiogram.filters import Command
from aiogram.types import Message
from sqlalchemy import func, select
from sqlalchemy.ext.asyncio import AsyncSession

from app.core.config import settings
from app.models import Like, Match, User, UserStatus, Video, VideoReport

router = Router()


def _is_admin(telegram_id: int) -> bool:
    return telegram_id in settings.admin_ids


@router.message(Command("admin"))
async def admin_help(message: Message) -> None:
    if not _is_admin(message.from_user.id):
        return
    await message.answer(
        "Админ-команды:\n"
        "/stats - статистика\n"
        "/block <telegram_id> - заблокировать\n"
        "/unblock <telegram_id> - разблокировать"
    )


@router.message(Command("stats"))
async def stats(message: Message, session: AsyncSession) -> None:
    if not _is_admin(message.from_user.id):
        return
    total_users = await _count(session, User)
    active_users = await _count_where(session, User, User.status == UserStatus.active)
    videos = await _count(session, Video)
    likes = await _count(session, Like)
    matches = await _count(session, Match)
    reports = await _count(session, VideoReport)
    premium_users = await _count_where(session, User, User.is_premium.is_(True))
    await message.answer(
        "Статистика:\n"
        f"Всего пользователей: {total_users}\n"
        f"Активных пользователей: {active_users}\n"
        f"Кружков: {videos}\n"
        f"Лайков: {likes}\n"
        f"Matches: {matches}\n"
        f"Жалоб на кружки: {reports}\n"
        f"Premium-пользователей: {premium_users}"
    )


@router.message(Command("block"))
async def block_user(message: Message, session: AsyncSession) -> None:
    if not _is_admin(message.from_user.id):
        return
    user = await _user_from_command(message, session)
    if user is None:
        await message.answer("Пользователь не найден. Формат: /block <telegram_id>")
        return
    user.status = UserStatus.blocked
    user.restricted_until = datetime.now(timezone.utc)
    await message.answer("Пользователь заблокирован.")


@router.message(Command("unblock"))
async def unblock_user(message: Message, session: AsyncSession) -> None:
    if not _is_admin(message.from_user.id):
        return
    user = await _user_from_command(message, session)
    if user is None:
        await message.answer("Пользователь не найден. Формат: /unblock <telegram_id>")
        return
    user.status = UserStatus.active
    user.restricted_until = None
    await message.answer("Пользователь разблокирован.")


async def _count(session: AsyncSession, model: type) -> int:
    result = await session.execute(select(func.count()).select_from(model))
    return int(result.scalar_one())


async def _count_where(session: AsyncSession, model: type, condition) -> int:
    result = await session.execute(select(func.count()).select_from(model).where(condition))
    return int(result.scalar_one())


async def _user_from_command(message: Message, session: AsyncSession) -> User | None:
    parts = (message.text or "").split(maxsplit=1)
    if len(parts) != 2 or not parts[1].isdigit():
        return None
    result = await session.execute(select(User).where(User.telegram_id == int(parts[1])))
    return result.scalar_one_or_none()

