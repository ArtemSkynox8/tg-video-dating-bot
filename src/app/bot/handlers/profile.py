from __future__ import annotations

from aiogram import F, Router
from aiogram.fsm.context import FSMContext
from aiogram.types import CallbackQuery, Message
from sqlalchemy.ext.asyncio import AsyncSession

from app.bot.keyboards.common import gender_keyboard, main_menu, preferred_gender_keyboard, profile_fields_keyboard
from app.bot.states import ProfileEdit
from app.models import Gender, PreferredGender
from app.repositories import users as user_repo
from app.repositories import videos as video_repo
from app.services.validation import is_valid_name, normalize_name

router = Router()


@router.message(F.text == "🎥 Перезаписать кружок")
async def ask_new_video(message: Message, state: FSMContext) -> None:
    await state.set_state(ProfileEdit.video)
    await message.answer("Отправьте новый кружок до 60 секунд.")


@router.message(ProfileEdit.video)
async def save_new_video(message: Message, state: FSMContext, session: AsyncSession) -> None:
    user = await user_repo.get_by_telegram_id(session, message.from_user.id)
    if user is None:
        await message.answer("Сначала завершите регистрацию: /start")
        return
    if message.video_note is None:
        await message.answer("Нужно отправить именно кружок.")
        return
    if message.video_note.duration > 60:
        await message.answer("Кружок должен быть до 60 секунд.")
        return
    await video_repo.set_active_video(
        session,
        user_id=user.id,
        telegram_file_id=message.video_note.file_id,
        duration=message.video_note.duration,
    )
    await state.clear()
    await message.answer("Кружок обновлен.", reply_markup=main_menu())


@router.message(F.text == "✏️ Поменять данные анкеты")
async def choose_profile_field(message: Message, state: FSMContext) -> None:
    await state.set_state(ProfileEdit.choosing_field)
    await message.answer("Что изменить?", reply_markup=profile_fields_keyboard())


@router.callback_query(F.data == "edit:name")
async def edit_name(callback: CallbackQuery, state: FSMContext) -> None:
    await state.set_state(ProfileEdit.name)
    await callback.message.edit_text("Введите новое имя.")
    await callback.answer()


@router.message(ProfileEdit.name)
async def save_name(message: Message, state: FSMContext, session: AsyncSession) -> None:
    user = await user_repo.get_by_telegram_id(session, message.from_user.id)
    if user is None:
        await message.answer("Сначала завершите регистрацию: /start")
        return
    if message.text is None or not is_valid_name(message.text):
        await message.answer("Имя должно быть от 2 до 30 символов: буквы, пробелы и дефисы.")
        return
    await user_repo.update_profile(session, user, name=normalize_name(message.text))
    await state.clear()
    await message.answer("Имя обновлено.", reply_markup=main_menu())


@router.callback_query(F.data == "edit:gender")
async def edit_gender(callback: CallbackQuery) -> None:
    await callback.message.edit_text("Выберите свой пол.", reply_markup=gender_keyboard("edit_gender"))
    await callback.answer()


@router.callback_query(F.data.startswith("edit_gender:"))
async def save_gender(callback: CallbackQuery, state: FSMContext, session: AsyncSession) -> None:
    user = await user_repo.get_by_telegram_id(session, callback.from_user.id)
    if user is None:
        await callback.answer("Сначала завершите регистрацию.", show_alert=True)
        return
    await user_repo.update_profile(session, user, gender=Gender(callback.data.split(":", 1)[1]))
    await state.clear()
    await callback.message.edit_text("Пол обновлен.")
    await callback.answer()


@router.callback_query(F.data == "edit:preferred_gender")
async def edit_preferred_gender(callback: CallbackQuery) -> None:
    await callback.message.edit_text(
        "Какие кружки хотите получать?",
        reply_markup=preferred_gender_keyboard("edit_preferred"),
    )
    await callback.answer()


@router.callback_query(F.data.startswith("edit_preferred:"))
async def save_preferred_gender(callback: CallbackQuery, state: FSMContext, session: AsyncSession) -> None:
    user = await user_repo.get_by_telegram_id(session, callback.from_user.id)
    if user is None:
        await callback.answer("Сначала завершите регистрацию.", show_alert=True)
        return
    await user_repo.update_profile(
        session,
        user,
        preferred_gender=PreferredGender(callback.data.split(":", 1)[1]),
    )
    await state.clear()
    await callback.message.edit_text("Настройки выдачи обновлены.")
    await callback.answer()


@router.message(F.text == "💎 Управление подпиской")
async def premium_stub(message: Message) -> None:
    await message.answer(
        "Premium-функции заложены в архитектуру. Подключение платежей можно добавить отдельным этапом."
    )

