from __future__ import annotations

from aiogram import F, Router
from aiogram.types import InlineKeyboardButton, InlineKeyboardMarkup, Message
from sqlalchemy.ext.asyncio import AsyncSession

from app.models import User
from app.repositories import users as user_repo
from app.services.matching import list_visible_matches

router = Router()


@router.message(F.text == "🚨 Пожаловаться")
async def report_from_menu(message: Message, session: AsyncSession) -> None:
    user = await user_repo.get_by_telegram_id(session, message.from_user.id)
    if user is None:
        await message.answer("Сначала завершите регистрацию: /start")
        return
    matches = await list_visible_matches(session, user.id)
    if not matches:
        await message.answer("Жалоба из меню доступна только на пользователей из взаимных лайков.")
        return

    keyboard = []
    for match in matches:
        contact_id = match.user2_id if match.user1_id == user.id else match.user1_id
        contact = await session.get(User, contact_id)
        if contact is None:
            continue
        keyboard.append(
            [
                InlineKeyboardButton(
                    text=f"{contact.name} (@{contact.username})",
                    callback_data=f"match_report:{match.id}:{contact.id}",
                )
            ]
        )

    await message.answer(
        "Выберите пользователя для жалобы.",
        reply_markup=InlineKeyboardMarkup(inline_keyboard=keyboard),
    )

