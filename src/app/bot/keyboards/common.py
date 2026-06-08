from __future__ import annotations

from aiogram.types import InlineKeyboardButton, InlineKeyboardMarkup, KeyboardButton, ReplyKeyboardMarkup

from app.models import Gender, PreferredGender, UserReportReason, VideoReportReason


def main_menu(matches_count: int = 0) -> ReplyKeyboardMarkup:
    return ReplyKeyboardMarkup(
        keyboard=[
            [KeyboardButton(text=f"📬 Взаимные лайки ({matches_count})")],
            [KeyboardButton(text="🎥 Перезаписать кружок")],
            [KeyboardButton(text="✏️ Поменять данные анкеты")],
            [KeyboardButton(text="💎 Управление подпиской")],
            [KeyboardButton(text="🚨 Пожаловаться")],
        ],
        resize_keyboard=True,
    )


def gender_keyboard(prefix: str) -> InlineKeyboardMarkup:
    return InlineKeyboardMarkup(
        inline_keyboard=[
            [
                InlineKeyboardButton(text="Мужской", callback_data=f"{prefix}:{Gender.male}"),
                InlineKeyboardButton(text="Женский", callback_data=f"{prefix}:{Gender.female}"),
            ]
        ]
    )


def preferred_gender_keyboard(prefix: str) -> InlineKeyboardMarkup:
    return InlineKeyboardMarkup(
        inline_keyboard=[
            [
                InlineKeyboardButton(text="Мужские", callback_data=f"{prefix}:{PreferredGender.male}"),
                InlineKeyboardButton(text="Женские", callback_data=f"{prefix}:{PreferredGender.female}"),
            ],
            [InlineKeyboardButton(text="Не важно", callback_data=f"{prefix}:{PreferredGender.any}")],
        ]
    )


def start_browsing_keyboard() -> InlineKeyboardMarkup:
    return InlineKeyboardMarkup(
        inline_keyboard=[[InlineKeyboardButton(text="▶️ Начать просмотр", callback_data="browse:start")]]
    )


def video_actions(video_id: int) -> InlineKeyboardMarkup:
    return InlineKeyboardMarkup(
        inline_keyboard=[
            [
                InlineKeyboardButton(text="❤️ Написать", callback_data=f"video:like:{video_id}"),
                InlineKeyboardButton(text="⏭ Следующий", callback_data=f"video:next:{video_id}"),
            ],
            [InlineKeyboardButton(text="🚨 Пожаловаться", callback_data=f"video:report:{video_id}")],
        ]
    )


def video_report_reasons(video_id: int) -> InlineKeyboardMarkup:
    labels = {
        VideoReportReason.spam: "Спам",
        VideoReportReason.adult: "18+",
        VideoReportReason.insult: "Оскорбления",
        VideoReportReason.fraud: "Мошенничество",
        VideoReportReason.other: "Другое",
    }
    return InlineKeyboardMarkup(
        inline_keyboard=[
            [InlineKeyboardButton(text=label, callback_data=f"video_report:{video_id}:{reason}")]
            for reason, label in labels.items()
        ]
    )


def user_report_reasons(match_id: int, user_id: int) -> InlineKeyboardMarkup:
    labels = {
        UserReportReason.spam: "Спам",
        UserReportReason.insult: "Оскорбления",
        UserReportReason.fraud: "Мошенничество",
        UserReportReason.unwanted_content: "Нежелательный контент",
        UserReportReason.other: "Другое",
    }
    return InlineKeyboardMarkup(
        inline_keyboard=[
            [
                InlineKeyboardButton(
                    text=label,
                    callback_data=f"user_report:{match_id}:{user_id}:{reason}",
                )
            ]
            for reason, label in labels.items()
        ]
    )


def profile_fields_keyboard() -> InlineKeyboardMarkup:
    return InlineKeyboardMarkup(
        inline_keyboard=[
            [InlineKeyboardButton(text="Имя", callback_data="edit:name")],
            [InlineKeyboardButton(text="Свой пол", callback_data="edit:gender")],
            [InlineKeyboardButton(text="Какие кружки получать", callback_data="edit:preferred_gender")],
        ]
    )

