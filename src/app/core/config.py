from __future__ import annotations

from functools import lru_cache
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

from pydantic import Field
from pydantic import field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    bot_token: str = Field(alias="BOT_TOKEN")
    database_url: str = Field(alias="DATABASE_URL")
    admin_ids_raw: str = Field(default="", alias="ADMIN_IDS")
    log_level: str = Field(default="INFO", alias="LOG_LEVEL")

    model_config = SettingsConfigDict(env_file=".env", env_file_encoding="utf-8", extra="ignore")

    @field_validator("database_url")
    @classmethod
    def normalize_database_url(cls, value: str) -> str:
        if value.startswith("postgresql://"):
            value = value.replace("postgresql://", "postgresql+asyncpg://", 1)

        parts = urlsplit(value)
        query = dict(parse_qsl(parts.query, keep_blank_values=True))
        if "sslmode" in query:
            sslmode = query["sslmode"].lower()
            if sslmode in {"true", "1", "yes", "on"}:
                query["sslmode"] = "require"
            elif sslmode in {"false", "0", "no", "off"}:
                query["sslmode"] = "disable"
        elif "ssl" in query:
            ssl = query.pop("ssl").lower()
            query["sslmode"] = "require" if ssl in {"true", "1", "yes", "on"} else "disable"
        return urlunsplit((parts.scheme, parts.netloc, parts.path, urlencode(query), parts.fragment))

    @property
    def admin_ids(self) -> set[int]:
        return {
            int(item.strip())
            for item in self.admin_ids_raw.split(",")
            if item.strip().isdigit()
        }


@lru_cache
def get_settings() -> Settings:
    return Settings()


settings = get_settings()
