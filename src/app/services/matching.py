from __future__ import annotations

from sqlalchemy import and_, or_, select
from sqlalchemy.dialects.postgresql import insert
from sqlalchemy.ext.asyncio import AsyncSession

from app.models import Like, Match, View, ViewAction


async def save_view(
    session: AsyncSession,
    *,
    viewer_id: int,
    video_id: int,
    viewed_user_id: int,
    action: ViewAction,
) -> None:
    stmt = (
        insert(View)
        .values(
            viewer_id=viewer_id,
            video_id=video_id,
            viewed_user_id=viewed_user_id,
            action=action,
        )
        .on_conflict_do_update(
            constraint="uq_views_viewer_video",
            set_={"action": action},
        )
    )
    await session.execute(stmt)


async def like_user(session: AsyncSession, *, from_user_id: int, to_user_id: int) -> Match | None:
    stmt = (
        insert(Like)
        .values(from_user_id=from_user_id, to_user_id=to_user_id)
        .on_conflict_do_nothing(constraint="uq_likes_from_to")
    )
    await session.execute(stmt)

    reverse_like = await session.execute(
        select(Like).where(Like.from_user_id == to_user_id, Like.to_user_id == from_user_id)
    )
    if reverse_like.scalar_one_or_none() is None:
        return None

    user1_id, user2_id = sorted((from_user_id, to_user_id))
    match_stmt = (
        insert(Match)
        .values(user1_id=user1_id, user2_id=user2_id)
        .on_conflict_do_nothing(constraint="uq_matches_pair")
        .returning(Match)
    )
    created = await session.execute(match_stmt)
    match = created.scalar_one_or_none()
    if match is not None:
        return match

    existing = await session.execute(
        select(Match).where(Match.user1_id == user1_id, Match.user2_id == user2_id)
    )
    return existing.scalar_one()


async def list_visible_matches(session: AsyncSession, user_id: int) -> list[Match]:
    result = await session.execute(
        select(Match)
        .where(
            or_(
                and_(Match.user1_id == user_id, Match.hidden_by_user1.is_(False)),
                and_(Match.user2_id == user_id, Match.hidden_by_user2.is_(False)),
            )
        )
        .order_by(Match.created_at.desc())
    )
    return list(result.scalars())


async def hide_match_for_user(session: AsyncSession, *, match_id: int, user_id: int) -> bool:
    result = await session.execute(select(Match).where(Match.id == match_id))
    match = result.scalar_one_or_none()
    if match is None:
        return False
    if match.user1_id == user_id:
        match.hidden_by_user1 = True
    elif match.user2_id == user_id:
        match.hidden_by_user2 = True
    else:
        return False
    await session.flush()
    return True

