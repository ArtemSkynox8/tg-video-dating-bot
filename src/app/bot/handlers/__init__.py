from aiogram import Router

from app.bot.handlers import admin, browse, matches, profile, registration, reports


def setup_routers() -> Router:
    router = Router()
    router.include_router(admin.router)
    router.include_router(registration.router)
    router.include_router(browse.router)
    router.include_router(matches.router)
    router.include_router(profile.router)
    router.include_router(reports.router)
    return router

