from __future__ import annotations

import re

NAME_RE = re.compile(r"^[A-Za-zА-Яа-яЁё -]{2,30}$")


def is_valid_name(value: str) -> bool:
    return bool(NAME_RE.fullmatch(value.strip()))


def normalize_name(value: str) -> str:
    return " ".join(value.strip().split())

