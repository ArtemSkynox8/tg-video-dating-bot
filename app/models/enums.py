from __future__ import annotations

from enum import StrEnum


class Gender(StrEnum):
    male = "male"
    female = "female"


class PreferredGender(StrEnum):
    male = "male"
    female = "female"
    any = "any"


class UserStatus(StrEnum):
    active = "active"
    blocked = "blocked"


class ViewAction(StrEnum):
    like = "like"
    next = "next"
    report = "report"


class VideoReportReason(StrEnum):
    spam = "spam"
    adult = "adult"
    insult = "insult"
    fraud = "fraud"
    other = "other"


class UserReportReason(StrEnum):
    spam = "spam"
    insult = "insult"
    fraud = "fraud"
    unwanted_content = "unwanted_content"
    other = "other"


class PaymentStatus(StrEnum):
    pending = "pending"
    paid = "paid"
    failed = "failed"

