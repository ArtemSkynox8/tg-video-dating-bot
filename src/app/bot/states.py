from aiogram.fsm.state import State, StatesGroup


class Registration(StatesGroup):
    name = State()
    gender = State()
    preferred_gender = State()
    video = State()


class ProfileEdit(StatesGroup):
    choosing_field = State()
    name = State()
    gender = State()
    preferred_gender = State()
    video = State()


class ReportVideo(StatesGroup):
    reason = State()


class ReportUser(StatesGroup):
    contact = State()
    reason = State()

