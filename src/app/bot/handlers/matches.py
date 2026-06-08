from __future__ import annotations

import asyncio

from aiogram import F, Router
from aiogram.types import CallbackQuery, InlineKeyboardButton, InlineKeyboardMarkup, Message
from sqlalchemy.ext.asyncio import AsyncSession

from app.bot.keyboards.common import user_report_reasons
from app.models import Match, User, UserReportReason
from app.repositories import users as user_repo
from app.repositories.videos import get_active_video_by_user
from app.services.matching import hide_match_for_user, list_visible_matches
from app.services.reports import report_user

router = Router()


@router.message(F.text.startswith("📬 Взаимные лайки"))
async def show_matches(message: Message, session: AsyncSession) -> None:
    user = await user_repo.get_by_telegram_id(session, message.from_user.id)
    if user is None:
        await message.answer("Сначала завершите регистрацию: /start")
        return
    matches = await list_visible_matches(session, user.id)
    if not matches:
        await message.answer("Пока нет активных взаимных лайков.")
        return

    for match in matches:
        contact_id = match.user2_id if match.user1_id == user.id else match.user1_id
        contact = await session.get(User, contact_id)
        if contact is None:
            continue
        await message.answer(
            f"{contact.name} (@{contact.username})",
            reply_markup=_match_keyboard(match.id, contact),
        )


@router.callback_query(F.data.startswith("match_video:"))
async def show_match_video(callback: CallbackQuery, session: AsyncSession) -> None:
    user_id = int(callback.data.rsplit(":", 1)[1])
    video = await get_active_video_by_user(session, user_id)
    if video is None:
        await callback.answer("У пользователя нет активного кружка.", show_alert=True)
        return
    sent = await callback.message.answer_video_note(video.telegram_file_id)
    await callback.answer()
    asyncio.create_task(_delete_after(sent, 60))


async def _delete_after(message: Message, seconds: int) -> None:
    await asyncio.sleep(seconds)
    try:
        await message.delete()
    except Exception:
        pass


@router.callback_query(F.data.startswith("match_hide:"))
async def hide_match(callback: CallbackQuery, session: AsyncSession) -> None:
    match_id = int(callback.data.rsplit(":", 1)[1])
    user = await user_repo.get_by_telegram_id(session, callback.from_user.id)
    if user is None:
        await callback.answer("Сначала завершите регистрацию.", show_alert=True)
        return
    if await hide_match_for_user(session, match_id=match_id, user_id=user.id):
        await callback.message.delete()
        await callback.answer("Контакт скрыт.")
    else:
        await callback.answer("Контакт не найден.", show_alert=True)


@router.callback_query(F.data.startswith("match_report:"))
async def choose_user_report_reason(callback: CallbackQuery, session: AsyncSession) -> None:
    _, match_id_raw, user_id_raw = callback.data.split(":", 2)
    await callback.message.edit_reply_markup(
        reply_markup=user_report_reasons(int(match_id_raw), int(user_id_raw))
    )
    await callback.answer()


@router.callback_query(F.data.startswith("user_report:"))
async def save_user_report(callback: CallbackQuery, session: AsyncSession) -> None:
    _, match_id_raw, reported_user_id_raw, reason_raw = callback.data.split(":", 3)
    reporter = await user_repo.get_by_telegram_id(session, callback.from_user.id)
    match = await session.get(Match, int(match_id_raw))
    reported_user_id = int(reported_user_id_raw)
    if reporter is None or match is None or reported_user_id not in {match.user1_id, match.user2_id}:
        await callback.answer("Жаловаться можно только на пользователей из взаимных лайков.", show_alert=True)
        return

    await report_user(
        session,
        reporter_id=reporter.id,
        reported_user_id=reported_user_id,
        match_id=match.id,
        reason=UserReportReason(reason_raw),
    )
    await callback.answer("Жалоба сохранена.")
    await callback.message.edit_reply_markup(reply_markup=None)


def _match_keyboard(match_id: int, contact: User) -> InlineKeyboardMarkup:
    profile_url = f"https://t.me/{contact.username}"
    return InlineKeyboardMarkup(
        inline_keyboard=[
            [
                InlineKeyboardButton(text="💬 Написать", url=profile_url),
                InlineKeyboardButton(text="🎥 Смотреть кружок", callback_data=f"match_video:{contact.id}"),
            ],
            [
                InlineKeyboardButton(text="🗑 Удалить из списка", callback_data=f"match_hide:{match_id}"),
                InlineKeyboardButton(
                    text="🚨 Пожаловаться",
                    callback_data=f"match_report:{match_id}:{contact.id}",
                ),
            ],
        ]
    )
