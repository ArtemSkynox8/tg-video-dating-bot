from __future__ import annotations

from datetime import datetime, timedelta, timezone

from sqlalchemy import func, select
from sqlalchemy.dialects.postgresql import insert
from sqlalchemy.ext.asyncio import AsyncSession

from app.models import User, UserReport, UserReportReason, VideoReport, VideoReportReason


async def report_video(
    session: AsyncSession,
    *,
    reporter_id: int,
    video_id: int,
    reported_user_id: int,
    reason: VideoReportReason,
) -> None:
    stmt = (
        insert(VideoReport)
        .values(
            reporter_id=reporter_id,
            video_id=video_id,
            reported_user_id=reported_user_id,
            reason=reason,
        )
        .on_conflict_do_nothing(constraint="uq_video_reports_reporter_video")
    )
    await session.execute(stmt)
    await apply_report_limits(session, reported_user_id)


async def report_user(
    session: AsyncSession,
    *,
    reporter_id: int,
    reported_user_id: int,
    match_id: int,
    reason: UserReportReason,
) -> None:
    stmt = (
        insert(UserReport)
        .values(
            reporter_id=reporter_id,
            reported_user_id=reported_user_id,
            match_id=match_id,
            reason=reason,
        )
        .on_conflict_do_nothing(constraint="uq_user_reports_reporter_user")
    )
    await session.execute(stmt)
    await apply_report_limits(session, reported_user_id)


async def apply_report_limits(session: AsyncSession, user_id: int) -> None:
    now = datetime.now(timezone.utc)
    unique_video_reports_24h = await _count_unique_video_reporters(session, user_id, now - timedelta(days=1))
    unique_video_reports_7d = await _count_unique_video_reporters(session, user_id, now - timedelta(days=7))
    unique_user_reports_24h = await _count_unique_user_reporters(session, user_id, now - timedelta(days=1))
    unique_user_reports_7d = await _count_unique_user_reporters(session, user_id, now - timedelta(days=7))

    reports_24h = unique_video_reports_24h + unique_user_reports_24h
    reports_7d = unique_video_reports_7d + unique_user_reports_7d
    restricted_until = None

    if reports_7d >= 30:
        restricted_until = now + timedelta(hours=72)
    elif reports_24h >= 10:
        restricted_until = now + timedelta(hours=24)

    if restricted_until is not None:
        user = await session.get(User, user_id)
        if user is not None:
            user.restricted_until = restricted_until
            await session.flush()


async def _count_unique_video_reporters(session: AsyncSession, user_id: int, since: datetime) -> int:
    result = await session.execute(
        select(func.count(func.distinct(VideoReport.reporter_id))).where(
            VideoReport.reported_user_id == user_id,
            VideoReport.created_at >= since,
        )
    )
    return int(result.scalar_one())


async def _count_unique_user_reporters(session: AsyncSession, user_id: int, since: datetime) -> int:
    result = await session.execute(
        select(func.count(func.distinct(UserReport.reporter_id))).where(
            UserReport.reported_user_id == user_id,
            UserReport.created_at >= since,
        )
    )
    return int(result.scalar_one())

