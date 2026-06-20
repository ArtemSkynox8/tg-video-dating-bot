create table if not exists chat_profiles (
    user_id bigint primary key references users(id) on delete cascade,
    character_id text not null,
    free_messages_used integer not null default 0,
    spicy_teaser_shown boolean not null default false,
    updated_at timestamptz not null default now()
);
create table if not exists chat_messages (
    id bigserial primary key,
    user_id bigint not null references users(id) on delete cascade,
    role text not null check (role in ('user', 'assistant')),
    content text not null,
    created_at timestamptz not null default now()
);
create index if not exists chat_messages_user_idx on chat_messages(user_id, id desc);
create table if not exists character_media (
    character_id text primary key,
    media_token text not null,
    updated_at timestamptz not null default now()
);
truncate table users cascade;
drop table if exists user_reports, video_reports, priority_queue, matches, likes, views, videos, referral_contact_opens cascade;
