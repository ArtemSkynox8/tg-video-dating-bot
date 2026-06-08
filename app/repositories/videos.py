from __future__ import annotations

from sqlalchemy import and_, exists, func, or_, select, update
from sqlalchemy.ext.asyncio import AsyncSession

from app.models import Gender, PreferredGender, User, UserStatus, Video, View


async def set_active_video(
    session: AsyncSession,
    *,
    user_id: int,
    telegram_file_id: str,
    duration: int,
) -> Video:
    await session.execute(update(Video).where(Video.user_id == user_id).values(is_active=False))
    video = Video(user_id=user_id, telegram_file_id=telegram_file_id, duration=duration, is_active=True)
    session.add(video)
    await session.flush()
    return video


async def get_active_video_by_user(session: AsyncSession, user_id: int) -> Video | None:
    result = await session.execute(
        select(Video).where(Video.user_id == user_id, Video.is_active.is_(True))
    )
    return result.scalar_one_or_none()


async def find_next_video(session: AsyncSession, viewer: User) -> Video | None:
    if viewer.preferred_gender == PreferredGender.male:
        gender_filter = User.gender == Gender.male
    elif viewer.preferred_gender == PreferredGender.female:
        gender_filter = User.gender == Gender.female
    else:
        gender_filter = True

    already_seen = exists().where(View.viewer_id == viewer.id, View.video_id == Video.id)
    result = await session.execute(
        select(Video)
        .join(User, User.id == Video.user_id)
        .where(
            Video.is_active.is_(True),
            Video.user_id != viewer.id,
            User.status == UserStatus.active,
            or_(User.restricted_until.is_(None), User.restricted_until < func.now()),
            gender_filter,
            ~already_seen,
            _viewer_matches_owner_preference(viewer),
        )
        .order_by(User.is_premium.desc(), Video.created_at.asc())
        .limit(1)
    )
    return result.scalar_one_or_none()


def _viewer_matches_owner_preference(viewer: User):
    if viewer.gender is None:
        return True
    return or_(
        User.preferred_gender == PreferredGender.any,
        and_(User.preferred_gender == PreferredGender.male, viewer.gender == Gender.male),
        and_(User.preferred_gender == PreferredGender.female, viewer.gender == Gender.female),
    )
