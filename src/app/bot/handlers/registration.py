from __future__ import annotations

from aiogram import F, Router
from aiogram.filters import CommandStart
from aiogram.fsm.context import FSMContext
from aiogram.types import CallbackQuery, Message
from sqlalchemy.ext.asyncio import AsyncSession

from app.bot.keyboards.common import (
    gender_keyboard,
    main_menu,
    preferred_gender_keyboard,
    start_browsing_keyboard,
)
from app.bot.states import Registration
from app.models import Gender, PreferredGender
from app.repositories import users as user_repo
from app.repositories import videos as video_repo
from app.services.validation import is_valid_name, normalize_name

router = Router()


@router.message(CommandStart())
async def start(message: Message, state: FSMContext, session: AsyncSession) -> None:
    tg_user = message.from_user
    if tg_user is None:
        return
    if not tg_user.username:
        await message.answer(
            "Для участия в знакомствах нужен Telegram username.\n"
            "Добавьте username в настройках Telegram и вернитесь в бот."
        )
        return

    user = await user_repo.get_or_create_user(session, tg_user.id, tg_user.username)
    active_video = await video_repo.get_active_video_by_user(session, user.id)
    if user_repo.is_registered(user) and active_video is not None:
        await message.answer("Вы уже зарегистрированы. Можно смотреть кружки.", reply_markup=main_menu())
        return

    await state.set_state(Registration.name)
    await message.answer("Введите имя: от 2 до 30 символов, буквы, пробелы и дефисы.")


@router.message(Registration.name)
async def registration_name(message: Message, state: FSMContext) -> None:
    if message.text is None or not is_valid_name(message.text):
        await message.answer("Имя должно быть от 2 до 30 символов: буквы, пробелы и дефисы.")
        return
    await state.update_data(name=normalize_name(message.text))
    await state.set_state(Registration.gender)
    await message.answer("Выберите свой пол.", reply_markup=gender_keyboard("reg_gender"))


@router.callback_query(Registration.gender, F.data.startswith("reg_gender:"))
async def registration_gender(callback: CallbackQuery, state: FSMContext) -> None:
    gender = Gender(callback.data.split(":", 1)[1])
    await state.update_data(gender=gender)
    await state.set_state(Registration.preferred_gender)
    await callback.message.edit_text(
        "Какие кружки хотите получать?",
        reply_markup=preferred_gender_keyboard("reg_preferred"),
    )
    await callback.answer()


@router.callback_query(Registration.preferred_gender, F.data.startswith("reg_preferred:"))
async def registration_preferred(callback: CallbackQuery, state: FSMContext) -> None:
    preferred_gender = PreferredGender(callback.data.split(":", 1)[1])
    await state.update_data(preferred_gender=preferred_gender)
    await state.set_state(Registration.video)
    await callback.message.edit_text("Отправьте Telegram video note, то есть кружок до 60 секунд.")
    await callback.answer()


@router.message(Registration.video)
async def registration_video(message: Message, state: FSMContext, session: AsyncSession) -> None:
    tg_user = message.from_user
    if tg_user is None or not tg_user.username:
        return
    if message.video_note is None:
        await message.answer("Нужно отправить именно кружок. Обычное видео и фото не принимаются.")
        return
    if message.video_note.duration > 60:
        await message.answer("Кружок должен быть до 60 секунд.")
        return

    data = await state.get_data()
    user = await user_repo.get_or_create_user(session, tg_user.id, tg_user.username)
    await user_repo.update_profile(
        session,
        user,
        name=data["name"],
        gender=data["gender"],
        preferred_gender=data["preferred_gender"],
    )
    await video_repo.set_active_video(
        session,
        user_id=user.id,
        telegram_file_id=message.video_note.file_id,
        duration=message.video_note.duration,
    )
    await state.clear()
    await message.answer(
        "✅ Анкета создана. Теперь вы можете смотреть кружки других пользователей.",
        reply_markup=main_menu(),
    )
    await message.answer("Начнем?", reply_markup=start_browsing_keyboard())

