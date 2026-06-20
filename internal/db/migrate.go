package db

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, schemaSQL)
	return err
}

const schemaSQL = `
create table if not exists users (
    id bigserial primary key,
    platform_user_id text not null unique,
    platform_chat_id text not null,
    platform_dialog_id text not null default '',
    profile_link text,
    contact_phone text,
    username text,
    name text,
    gender text,
    preferred_gender text,
    flow_state text not null default '',
    is_premium boolean not null default false,
    status text not null default 'active',
    restricted_until timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

alter table users add column if not exists flow_state text not null default '';
alter table users add column if not exists platform_dialog_id text not null default '';
alter table users add column if not exists contact_phone text;
alter table users add column if not exists premium_offer_chat_id text;
alter table users add column if not exists premium_offer_message_id text;
alter table users add column if not exists referrer_user_id bigint references users(id) on delete set null;
alter table users add column if not exists referral_contact_credits integer not null default 0;
alter table users add column if not exists referral_rewarded_at timestamptz;
alter table users add column if not exists ad_tag text;

create table if not exists videos (
    id bigserial primary key,
    user_id bigint not null references users(id) on delete cascade,
    platform_media_id text not null,
    storage_url text,
    duration integer not null default 0,
    is_active boolean not null default true,
    created_at timestamptz not null default now()
);

create unique index if not exists videos_one_active_per_user
on videos(user_id)
where is_active = true;

create table if not exists views (
    id bigserial primary key,
    viewer_id bigint not null references users(id) on delete cascade,
    video_id bigint not null references videos(id) on delete cascade,
    viewed_user_id bigint not null references users(id) on delete cascade,
    action text not null check (action in ('like', 'next', 'report')),
    created_at timestamptz not null default now(),
    unique (viewer_id, video_id)
);

create table if not exists likes (
    id bigserial primary key,
    from_user_id bigint not null references users(id) on delete cascade,
    to_user_id bigint not null references users(id) on delete cascade,
    created_at timestamptz not null default now(),
    unique (from_user_id, to_user_id)
);

create table if not exists matches (
    id bigserial primary key,
    user1_id bigint not null references users(id) on delete cascade,
    user2_id bigint not null references users(id) on delete cascade,
    hidden_by_user1 boolean not null default false,
    hidden_by_user2 boolean not null default false,
    created_at timestamptz not null default now(),
    unique (user1_id, user2_id),
    check (user1_id < user2_id)
);

create table if not exists priority_queue (
    id bigserial primary key,
    target_user_id bigint not null references users(id) on delete cascade,
    candidate_user_id bigint not null references users(id) on delete cascade,
    reason text not null,
    expires_at timestamptz not null,
    created_at timestamptz not null default now(),
    unique (target_user_id, candidate_user_id)
);

create table if not exists video_reports (
    id bigserial primary key,
    reporter_id bigint not null references users(id) on delete cascade,
    video_id bigint not null references videos(id) on delete cascade,
    reported_user_id bigint not null references users(id) on delete cascade,
    reason text not null,
    created_at timestamptz not null default now(),
    unique (reporter_id, video_id)
);

create table if not exists user_reports (
    id bigserial primary key,
    reporter_id bigint not null references users(id) on delete cascade,
    reported_user_id bigint not null references users(id) on delete cascade,
    match_id bigint not null references matches(id) on delete cascade,
    reason text not null,
    created_at timestamptz not null default now(),
    unique (reporter_id, reported_user_id, match_id)
);

create table if not exists premium_payments (
    id bigserial primary key,
    user_id bigint not null references users(id) on delete cascade,
    amount numeric(12, 2) not null,
    provider text not null,
    external_id text,
    status text not null,
    plan text,
    period_days integer not null default 7,
    payment_method_id text,
    reason text not null default 'initial',
    created_at timestamptz not null default now()
);

alter table premium_payments add column if not exists external_id text;
alter table premium_payments add column if not exists plan text;
alter table premium_payments add column if not exists period_days integer not null default 7;
alter table premium_payments add column if not exists payment_method_id text;
alter table premium_payments add column if not exists reason text not null default 'initial';

create table if not exists premium_subscriptions (
    user_id bigint primary key references users(id) on delete cascade,
    plan text not null,
    amount numeric(12, 2) not null,
    period_days integer not null,
    payment_method_id text,
    active boolean not null default true,
    current_period_until timestamptz not null,
    next_charge_at timestamptz not null,
    updated_at timestamptz not null default now()
);

create table if not exists referral_contact_opens (
    id bigserial primary key,
    user_id bigint not null references users(id) on delete cascade,
    opened_user_id bigint not null references users(id) on delete cascade,
    created_at timestamptz not null default now(),
    unique (user_id, opened_user_id)
);

create table if not exists user_action_logs (
    id bigserial primary key,
    user_id bigint references users(id) on delete set null,
    action text not null,
    payload jsonb not null default '{}'::jsonb,
    created_at timestamptz not null default now()
);

create index if not exists users_status_restricted_idx on users(status, restricted_until);
create index if not exists videos_active_idx on videos(is_active);
create index if not exists views_viewer_idx on views(viewer_id);
create index if not exists likes_to_user_idx on likes(to_user_id);
create index if not exists matches_user1_idx on matches(user1_id);
create index if not exists matches_user2_idx on matches(user2_id);
create index if not exists priority_queue_target_idx on priority_queue(target_user_id, expires_at);
create index if not exists video_reports_reported_idx on video_reports(reported_user_id, created_at);
create index if not exists premium_payments_user_status_idx on premium_payments(user_id, status, created_at desc);
create index if not exists premium_payments_external_idx on premium_payments(external_id);
create index if not exists premium_subscriptions_due_idx on premium_subscriptions(active, next_charge_at);
create index if not exists users_referrer_idx on users(referrer_user_id);
create index if not exists users_ad_tag_idx on users(ad_tag);
create index if not exists referral_contact_opens_user_idx on referral_contact_opens(user_id, created_at desc);
create index if not exists user_action_logs_action_idx on user_action_logs(action, created_at desc);

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

create table if not exists app_data_migrations (
    name text primary key,
    applied_at timestamptz not null default now()
);

do $$
begin
    if not exists (select 1 from app_data_migrations where name='ai_characters_v1_reset') then
        delete from users;
        delete from character_media;
        insert into app_data_migrations(name) values('ai_characters_v1_reset');
    end if;
end $$;

-- Old user-recorded dating data is intentionally removed by the new AI-character flow.
drop table if exists user_reports cascade;
drop table if exists video_reports cascade;
drop table if exists priority_queue cascade;
drop table if exists matches cascade;
drop table if exists likes cascade;
drop table if exists views cascade;
drop table if exists videos cascade;
drop table if exists referral_contact_opens cascade;
`
