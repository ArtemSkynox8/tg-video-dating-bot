from __future__ import annotations

from collections.abc import Sequence

import sqlalchemy as sa
from alembic import op

revision: str = "20260608_0001"
down_revision: str | None = None
branch_labels: str | Sequence[str] | None = None
depends_on: str | Sequence[str] | None = None


def upgrade() -> None:
    gender = sa.Enum("male", "female", name="gender")
    preferred_gender = sa.Enum("male", "female", "any", name="preferred_gender")
    user_status = sa.Enum("active", "blocked", name="user_status")
    view_action = sa.Enum("like", "next", "report", name="view_action")
    video_report_reason = sa.Enum("spam", "adult", "insult", "fraud", "other", name="video_report_reason")
    user_report_reason = sa.Enum(
        "spam",
        "insult",
        "fraud",
        "unwanted_content",
        "other",
        name="user_report_reason",
    )
    payment_status = sa.Enum("pending", "paid", "failed", name="payment_status")

    gender.create(op.get_bind(), checkfirst=True)
    preferred_gender.create(op.get_bind(), checkfirst=True)
    user_status.create(op.get_bind(), checkfirst=True)
    view_action.create(op.get_bind(), checkfirst=True)
    video_report_reason.create(op.get_bind(), checkfirst=True)
    user_report_reason.create(op.get_bind(), checkfirst=True)
    payment_status.create(op.get_bind(), checkfirst=True)

    op.create_table(
        "users",
        sa.Column("id", sa.Integer(), primary_key=True),
        sa.Column("telegram_id", sa.BigInteger(), nullable=False),
        sa.Column("username", sa.String(length=64), nullable=False),
        sa.Column("name", sa.String(length=30), nullable=True),
        sa.Column("gender", gender, nullable=True),
        sa.Column("preferred_gender", preferred_gender, nullable=True),
        sa.Column("is_premium", sa.Boolean(), nullable=False, server_default=sa.false()),
        sa.Column("status", user_status, nullable=False, server_default="active"),
        sa.Column("restricted_until", sa.DateTime(timezone=True), nullable=True),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.Column("updated_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
    )
    op.create_index("ix_users_telegram_id", "users", ["telegram_id"], unique=True)

    op.create_table(
        "videos",
        sa.Column("id", sa.Integer(), primary_key=True),
        sa.Column("user_id", sa.Integer(), sa.ForeignKey("users.id", ondelete="CASCADE"), nullable=False),
        sa.Column("telegram_file_id", sa.String(length=255), nullable=False),
        sa.Column("duration", sa.Integer(), nullable=False),
        sa.Column("is_active", sa.Boolean(), nullable=False, server_default=sa.true()),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
    )
    op.create_index("ix_videos_user_id", "videos", ["user_id"])

    op.create_table(
        "views",
        sa.Column("id", sa.Integer(), primary_key=True),
        sa.Column("viewer_id", sa.Integer(), sa.ForeignKey("users.id", ondelete="CASCADE"), nullable=False),
        sa.Column("video_id", sa.Integer(), sa.ForeignKey("videos.id", ondelete="CASCADE"), nullable=False),
        sa.Column("viewed_user_id", sa.Integer(), sa.ForeignKey("users.id", ondelete="CASCADE"), nullable=False),
        sa.Column("action", view_action, nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.UniqueConstraint("viewer_id", "video_id", name="uq_views_viewer_video"),
    )
    op.create_index("ix_views_viewer_id", "views", ["viewer_id"])
    op.create_index("ix_views_video_id", "views", ["video_id"])
    op.create_index("ix_views_viewed_user_id", "views", ["viewed_user_id"])

    op.create_table(
        "likes",
        sa.Column("id", sa.Integer(), primary_key=True),
        sa.Column("from_user_id", sa.Integer(), sa.ForeignKey("users.id", ondelete="CASCADE"), nullable=False),
        sa.Column("to_user_id", sa.Integer(), sa.ForeignKey("users.id", ondelete="CASCADE"), nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.UniqueConstraint("from_user_id", "to_user_id", name="uq_likes_from_to"),
    )
    op.create_index("ix_likes_from_user_id", "likes", ["from_user_id"])
    op.create_index("ix_likes_to_user_id", "likes", ["to_user_id"])

    op.create_table(
        "matches",
        sa.Column("id", sa.Integer(), primary_key=True),
        sa.Column("user1_id", sa.Integer(), sa.ForeignKey("users.id", ondelete="CASCADE"), nullable=False),
        sa.Column("user2_id", sa.Integer(), sa.ForeignKey("users.id", ondelete="CASCADE"), nullable=False),
        sa.Column("hidden_by_user1", sa.Boolean(), nullable=False, server_default=sa.false()),
        sa.Column("hidden_by_user2", sa.Boolean(), nullable=False, server_default=sa.false()),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.UniqueConstraint("user1_id", "user2_id", name="uq_matches_pair"),
    )
    op.create_index("ix_matches_user1_id", "matches", ["user1_id"])
    op.create_index("ix_matches_user2_id", "matches", ["user2_id"])

    op.create_table(
        "video_reports",
        sa.Column("id", sa.Integer(), primary_key=True),
        sa.Column("reporter_id", sa.Integer(), sa.ForeignKey("users.id", ondelete="CASCADE"), nullable=False),
        sa.Column("video_id", sa.Integer(), sa.ForeignKey("videos.id", ondelete="CASCADE"), nullable=False),
        sa.Column("reported_user_id", sa.Integer(), sa.ForeignKey("users.id", ondelete="CASCADE"), nullable=False),
        sa.Column("reason", video_report_reason, nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.UniqueConstraint("reporter_id", "video_id", name="uq_video_reports_reporter_video"),
    )
    op.create_index("ix_video_reports_reporter_id", "video_reports", ["reporter_id"])
    op.create_index("ix_video_reports_video_id", "video_reports", ["video_id"])
    op.create_index("ix_video_reports_reported_user_id", "video_reports", ["reported_user_id"])

    op.create_table(
        "user_reports",
        sa.Column("id", sa.Integer(), primary_key=True),
        sa.Column("reporter_id", sa.Integer(), sa.ForeignKey("users.id", ondelete="CASCADE"), nullable=False),
        sa.Column("reported_user_id", sa.Integer(), sa.ForeignKey("users.id", ondelete="CASCADE"), nullable=False),
        sa.Column("match_id", sa.Integer(), sa.ForeignKey("matches.id", ondelete="CASCADE"), nullable=False),
        sa.Column("reason", user_report_reason, nullable=False),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
        sa.UniqueConstraint("reporter_id", "reported_user_id", name="uq_user_reports_reporter_user"),
    )
    op.create_index("ix_user_reports_reporter_id", "user_reports", ["reporter_id"])
    op.create_index("ix_user_reports_reported_user_id", "user_reports", ["reported_user_id"])
    op.create_index("ix_user_reports_match_id", "user_reports", ["match_id"])

    op.create_table(
        "premium_payments",
        sa.Column("id", sa.Integer(), primary_key=True),
        sa.Column("user_id", sa.Integer(), sa.ForeignKey("users.id", ondelete="CASCADE"), nullable=False),
        sa.Column("amount", sa.Integer(), nullable=False),
        sa.Column("provider", sa.String(), nullable=False),
        sa.Column("status", payment_status, nullable=False, server_default="pending"),
        sa.Column("created_at", sa.DateTime(timezone=True), server_default=sa.func.now(), nullable=False),
    )
    op.create_index("ix_premium_payments_user_id", "premium_payments", ["user_id"])


def downgrade() -> None:
    op.drop_index("ix_premium_payments_user_id", table_name="premium_payments")
    op.drop_table("premium_payments")
    op.drop_index("ix_user_reports_match_id", table_name="user_reports")
    op.drop_index("ix_user_reports_reported_user_id", table_name="user_reports")
    op.drop_index("ix_user_reports_reporter_id", table_name="user_reports")
    op.drop_table("user_reports")
    op.drop_index("ix_video_reports_reported_user_id", table_name="video_reports")
    op.drop_index("ix_video_reports_video_id", table_name="video_reports")
    op.drop_index("ix_video_reports_reporter_id", table_name="video_reports")
    op.drop_table("video_reports")
    op.drop_index("ix_matches_user2_id", table_name="matches")
    op.drop_index("ix_matches_user1_id", table_name="matches")
    op.drop_table("matches")
    op.drop_index("ix_likes_to_user_id", table_name="likes")
    op.drop_index("ix_likes_from_user_id", table_name="likes")
    op.drop_table("likes")
    op.drop_index("ix_views_viewed_user_id", table_name="views")
    op.drop_index("ix_views_video_id", table_name="views")
    op.drop_index("ix_views_viewer_id", table_name="views")
    op.drop_table("views")
    op.drop_index("ix_videos_user_id", table_name="videos")
    op.drop_table("videos")
    op.drop_index("ix_users_telegram_id", table_name="users")
    op.drop_table("users")

    for enum_name in (
        "payment_status",
        "user_report_reason",
        "video_report_reason",
        "view_action",
        "user_status",
        "preferred_gender",
        "gender",
    ):
        sa.Enum(name=enum_name).drop(op.get_bind(), checkfirst=True)

