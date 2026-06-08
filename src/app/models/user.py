from __future__ import annotations

from datetime import datetime

from sqlalchemy import BigInteger, Boolean, DateTime, Enum, String
from sqlalchemy.orm import Mapped, mapped_column, relationship

from app.db.base import Base, TimestampMixin, UpdatedAtMixin
from app.models.enums import Gender, PreferredGender, UserStatus


class User(Base, TimestampMixin, UpdatedAtMixin):
    __tablename__ = "users"

    id: Mapped[int] = mapped_column(primary_key=True)
    telegram_id: Mapped[int] = mapped_column(BigInteger, unique=True, index=True, nullable=False)
    username: Mapped[str] = mapped_column(String(64), nullable=False)
    name: Mapped[str | None] = mapped_column(String(30))
    gender: Mapped[Gender | None] = mapped_column(Enum(Gender, name="gender"))
    preferred_gender: Mapped[PreferredGender | None] = mapped_column(
        Enum(PreferredGender, name="preferred_gender")
    )
    is_premium: Mapped[bool] = mapped_column(Boolean, default=False, nullable=False)
    status: Mapped[UserStatus] = mapped_column(
        Enum(UserStatus, name="user_status"),
        default=UserStatus.active,
        nullable=False,
    )
    restricted_until: Mapped[datetime | None] = mapped_column(DateTime(timezone=True))

    videos = relationship("Video", back_populates="user")

