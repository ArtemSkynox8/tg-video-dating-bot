from __future__ import annotations

from aiogram import F, Router
from aiogram.types import CallbackQuery, Message
from sqlalchemy.ext.asyncio import AsyncSession

from app.bot.keyboards.common import (
    main_menu,
    start_browsing_keyboard,
    video_actions,
    video_report_reasons,
)
from app.models import User, Video, VideoReportReason, ViewAction
from app.repositories import users as user_repo
from app.repositories import videos as video_repo
from app.services import matching
from app.services.reports import report_video

router = Router()


@router.callback_query(F.data == "browse:start")
async def start_browse_callback(callback: CallbackQuery, session: AsyncSession) -> None:
    await _show_next_video(callback.from_user.id, callback.message, session)
    await callback.answer()


@router.message(F.text == "▶️ Начать просмотр")
async def start_browse_message(message: Message, session: AsyncSession) -> None:
    await _show_next_video(message.from_user.id, message, session)


@router.callback_query(F.data.startswith("video:next:"))
async def next_video(callback: CallbackQuery, session: AsyncSession) -> None:
    video_id = int(callback.data.rsplit(":", 1)[1])
    await _save_action(callback.from_user.id, video_id, ViewAction.next, session)
    await _delete_callback_message(callback)
    await _show_next_video(callback.from_user.id, callback.message, session)
    await callback.answer()


@router.callback_query(F.data.startswith("video:like:"))
async def like_video(callback: CallbackQuery, session: AsyncSession) -> None:
    video_id = int(callback.data.rsplit(":", 1)[1])
    viewer = await user_repo.get_by_telegram_id(session, callback.from_user.id)
    video = await session.get(Video, video_id)
    if viewer is None or video is None:
        await callback.answer("Кружок уже недоступен.", show_alert=True)
        return

    await matching.save_view(
        session,
        viewer_id=viewer.id,
        video_id=video.id,
        viewed_user_id=video.user_id,
        action=ViewAction.like,
    )
    match = await matching.like_user(session, from_user_id=viewer.id, to_user_id=video.user_id)
    await _delete_callback_message(callback)

    if match is not None:
        await callback.message.answer(
            "❤️ У вас новый взаимный лайк!",
            reply_markup=main_menu(),
        )
        await callback.message.answer(
            "Можно открыть раздел взаимных лайков или продолжить просмотр.",
            reply_markup=start_browsing_keyboard(),
        )
    else:
        await _show_next_video(callback.from_user.id, callback.message, session)
    await callback.answer()


@router.callback_query(F.data.startswith("video:report:"))
async def choose_report_reason(callback: CallbackQuery) -> None:
    video_id = int(callback.data.rsplit(":", 1)[1])
    await callback.message.edit_reply_markup(reply_markup=video_report_reasons(video_id))
    await callback.answer()


@router.callback_query(F.data.startswith("video_report:"))
async def save_video_report(callback: CallbackQuery, session: AsyncSession) -> None:
    _, video_id_raw, reason_raw = callback.data.split(":", 2)
    video_id = int(video_id_raw)
    reason = VideoReportReason(reason_raw)
    viewer = await user_repo.get_by_telegram_id(session, callback.from_user.id)
    video = await session.get(Video, video_id)
    if viewer is None or video is None:
        await callback.answer("Кружок уже недоступен.", show_alert=True)
        return

    await matching.save_view(
        session,
        viewer_id=viewer.id,
        video_id=video.id,
        viewed_user_id=video.user_id,
        action=ViewAction.report,
    )
    await report_video(
        session,
        reporter_id=viewer.id,
        video_id=video.id,
        reported_user_id=video.user_id,
        reason=reason,
    )
    await _delete_callback_message(callback)
    await _show_next_video(callback.from_user.id, callback.message, session)
    await callback.answer("Жалоба сохранена.")


async def _show_next_video(telegram_id: int, message: Message, session: AsyncSession) -> None:
    viewer = await user_repo.get_by_telegram_id(session, telegram_id)
    if viewer is None or not user_repo.is_registered(viewer):
        await message.answer("Сначала завершите регистрацию: /start")
        return
    if user_repo.is_restricted(viewer):
        await message.answer("Просмотр новых кружков временно ограничен.", reply_markup=main_menu())
        return

    video = await video_repo.find_next_video(session, viewer)
    if video is None:
        await message.answer("Подходящих новых кружков пока нет.", reply_markup=main_menu())
        return

    await message.answer_video_note(video.telegram_file_id, reply_markup=video_actions(video.id))


async def _save_action(
    telegram_id: int,
    video_id: int,
    action: ViewAction,
    session: AsyncSession,
) -> None:
    viewer = await user_repo.get_by_telegram_id(session, telegram_id)
    video = await session.get(Video, video_id)
    if viewer is None or video is None:
        return
    await matching.save_view(
        session,
        viewer_id=viewer.id,
        video_id=video.id,
        viewed_user_id=video.user_id,
        action=action,
    )


async def _delete_callback_message(callback: CallbackQuery) -> None:
    try:
        await callback.message.delete()
    except Exception:
        await callback.message.edit_reply_markup(reply_markup=None)

