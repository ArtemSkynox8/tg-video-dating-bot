from __future__ import annotations

from functools import lru_cache
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    bot_token: str = Field(alias="BOT_TOKEN")
    database_url: str = Field(alias="DATABASE_URL")
    admin_ids_raw: str = Field(default="", alias="ADMIN_IDS")
    log_level: str = Field(default="INFO", alias="LOG_LEVEL")

    model_config = SettingsConfigDict(env_file=".env", env_file_encoding="utf-8", extra="ignore")

    @property
    def normalized_database_url(self) -> str:
        value = self.database_url
        if value.startswith("postgresql://"):
            value = value.replace("postgresql://", "postgresql+asyncpg://", 1)

        parts = urlsplit(value)
        query = dict(parse_qsl(parts.query, keep_blank_values=True))
        query.pop("ssl", None)
        query.pop("sslmode", None)
        return urlunsplit((parts.scheme, parts.netloc, parts.path, urlencode(query), parts.fragment))

    @property
    def database_connect_args(self) -> dict[str, bool]:
        parts = urlsplit(self.database_url)
        query = dict(parse_qsl(parts.query, keep_blank_values=True))
        ssl_value = query.get("sslmode", query.get("ssl", "")).lower()
        if ssl_value and ssl_value not in {"disable", "false", "0", "no", "off"}:
            return {"ssl": True}
        return {}

    @property
    def database_host(self) -> str | None:
        return urlsplit(self.normalized_database_url).hostname

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
