from __future__ import annotations

from sqlalchemy import Boolean, Enum, ForeignKey, UniqueConstraint
from sqlalchemy.orm import Mapped, mapped_column

from app.db.base import Base, TimestampMixin
from app.models.enums import PaymentStatus, UserReportReason, VideoReportReason, ViewAction


class View(Base, TimestampMixin):
    __tablename__ = "views"
    __table_args__ = (
        UniqueConstraint("viewer_id", "video_id", name="uq_views_viewer_video"),
    )

    id: Mapped[int] = mapped_column(primary_key=True)
    viewer_id: Mapped[int] = mapped_column(ForeignKey("users.id", ondelete="CASCADE"), index=True)
    video_id: Mapped[int] = mapped_column(ForeignKey("videos.id", ondelete="CASCADE"), index=True)
    viewed_user_id: Mapped[int] = mapped_column(ForeignKey("users.id", ondelete="CASCADE"), index=True)
    action: Mapped[ViewAction] = mapped_column(Enum(ViewAction, name="view_action"), nullable=False)


class Like(Base, TimestampMixin):
    __tablename__ = "likes"
    __table_args__ = (
        UniqueConstraint("from_user_id", "to_user_id", name="uq_likes_from_to"),
    )

    id: Mapped[int] = mapped_column(primary_key=True)
    from_user_id: Mapped[int] = mapped_column(ForeignKey("users.id", ondelete="CASCADE"), index=True)
    to_user_id: Mapped[int] = mapped_column(ForeignKey("users.id", ondelete="CASCADE"), index=True)


class Match(Base, TimestampMixin):
    __tablename__ = "matches"
    __table_args__ = (
        UniqueConstraint("user1_id", "user2_id", name="uq_matches_pair"),
    )

    id: Mapped[int] = mapped_column(primary_key=True)
    user1_id: Mapped[int] = mapped_column(ForeignKey("users.id", ondelete="CASCADE"), index=True)
    user2_id: Mapped[int] = mapped_column(ForeignKey("users.id", ondelete="CASCADE"), index=True)
    hidden_by_user1: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    hidden_by_user2: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)


class VideoReport(Base, TimestampMixin):
    __tablename__ = "video_reports"
    __table_args__ = (
        UniqueConstraint("reporter_id", "video_id", name="uq_video_reports_reporter_video"),
    )

    id: Mapped[int] = mapped_column(primary_key=True)
    reporter_id: Mapped[int] = mapped_column(ForeignKey("users.id", ondelete="CASCADE"), index=True)
    video_id: Mapped[int] = mapped_column(ForeignKey("videos.id", ondelete="CASCADE"), index=True)
    reported_user_id: Mapped[int] = mapped_column(ForeignKey("users.id", ondelete="CASCADE"), index=True)
    reason: Mapped[VideoReportReason] = mapped_column(
        Enum(VideoReportReason, name="video_report_reason"),
        nullable=False,
    )


class UserReport(Base, TimestampMixin):
    __tablename__ = "user_reports"
    __table_args__ = (
        UniqueConstraint("reporter_id", "reported_user_id", name="uq_user_reports_reporter_user"),
    )

    id: Mapped[int] = mapped_column(primary_key=True)
    reporter_id: Mapped[int] = mapped_column(ForeignKey("users.id", ondelete="CASCADE"), index=True)
    reported_user_id: Mapped[int] = mapped_column(ForeignKey("users.id", ondelete="CASCADE"), index=True)
    match_id: Mapped[int] = mapped_column(ForeignKey("matches.id", ondelete="CASCADE"), index=True)
    reason: Mapped[UserReportReason] = mapped_column(
        Enum(UserReportReason, name="user_report_reason"),
        nullable=False,
    )


class PremiumPayment(Base, TimestampMixin):
    __tablename__ = "premium_payments"

    id: Mapped[int] = mapped_column(primary_key=True)
    user_id: Mapped[int] = mapped_column(ForeignKey("users.id", ondelete="CASCADE"), index=True)
    amount: Mapped[int] = mapped_column(nullable=False)
    provider: Mapped[str] = mapped_column(nullable=False)
    status: Mapped[PaymentStatus] = mapped_column(
        Enum(PaymentStatus, name="payment_status"),
        default=PaymentStatus.pending,
        nullable=False,
    )

