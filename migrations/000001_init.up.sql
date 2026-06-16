create table users (
    id bigserial primary key,
    platform_user_id text not null unique,
    platform_chat_id text not null,
    profile_link text,
    username text,
    name text,
    gender text,
    preferred_gender text,
    flow_state text not null default '',
    is_premium boolean not null default false,
    referrer_user_id bigint references users(id) on delete set null,
    referral_contact_credits integer not null default 0,
    referral_rewarded_at timestamptz,
    ad_tag text,
    status text not null default 'active',
    restricted_until timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create table videos (
    id bigserial primary key,
    user_id bigint not null references users(id) on delete cascade,
    platform_media_id text not null,
    storage_url text,
    duration integer not null default 0,
    is_active boolean not null default true,
    created_at timestamptz not null default now()
);

create unique index videos_one_active_per_user
on videos(user_id)
where is_active = true;

create table views (
    id bigserial primary key,
    viewer_id bigint not null references users(id) on delete cascade,
    video_id bigint not null references videos(id) on delete cascade,
    viewed_user_id bigint not null references users(id) on delete cascade,
    action text not null check (action in ('like', 'next', 'report')),
    created_at timestamptz not null default now(),
    unique (viewer_id, video_id)
);

create table likes (
    id bigserial primary key,
    from_user_id bigint not null references users(id) on delete cascade,
    to_user_id bigint not null references users(id) on delete cascade,
    created_at timestamptz not null default now(),
    unique (from_user_id, to_user_id)
);

create table matches (
    id bigserial primary key,
    user1_id bigint not null references users(id) on delete cascade,
    user2_id bigint not null references users(id) on delete cascade,
    hidden_by_user1 boolean not null default false,
    hidden_by_user2 boolean not null default false,
    created_at timestamptz not null default now(),
    unique (user1_id, user2_id),
    check (user1_id < user2_id)
);

create table priority_queue (
    id bigserial primary key,
    target_user_id bigint not null references users(id) on delete cascade,
    candidate_user_id bigint not null references users(id) on delete cascade,
    reason text not null,
    expires_at timestamptz not null,
    created_at timestamptz not null default now(),
    unique (target_user_id, candidate_user_id)
);

create table video_reports (
    id bigserial primary key,
    reporter_id bigint not null references users(id) on delete cascade,
    video_id bigint not null references videos(id) on delete cascade,
    reported_user_id bigint not null references users(id) on delete cascade,
    reason text not null,
    created_at timestamptz not null default now(),
    unique (reporter_id, video_id)
);

create table user_reports (
    id bigserial primary key,
    reporter_id bigint not null references users(id) on delete cascade,
    reported_user_id bigint not null references users(id) on delete cascade,
    match_id bigint not null references matches(id) on delete cascade,
    reason text not null,
    created_at timestamptz not null default now(),
    unique (reporter_id, reported_user_id, match_id)
);

create table premium_payments (
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

create table premium_subscriptions (
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

create table referral_contact_opens (
    id bigserial primary key,
    user_id bigint not null references users(id) on delete cascade,
    opened_user_id bigint not null references users(id) on delete cascade,
    created_at timestamptz not null default now(),
    unique (user_id, opened_user_id)
);

create table user_action_logs (
    id bigserial primary key,
    user_id bigint references users(id) on delete set null,
    action text not null,
    payload jsonb not null default '{}'::jsonb,
    created_at timestamptz not null default now()
);

create index users_status_restricted_idx on users(status, restricted_until);
create index videos_active_idx on videos(is_active);
create index views_viewer_idx on views(viewer_id);
create index likes_to_user_idx on likes(to_user_id);
create index matches_user1_idx on matches(user1_id);
create index matches_user2_idx on matches(user2_id);
create index priority_queue_target_idx on priority_queue(target_user_id, expires_at);
create index video_reports_reported_idx on video_reports(reported_user_id, created_at);
create index premium_payments_user_status_idx on premium_payments(user_id, status, created_at desc);
create index premium_payments_external_idx on premium_payments(external_id);
create index premium_subscriptions_due_idx on premium_subscriptions(active, next_charge_at);
create index users_referrer_idx on users(referrer_user_id);
create index users_ad_tag_idx on users(ad_tag);
create index referral_contact_opens_user_idx on referral_contact_opens(user_id, created_at desc);
create index user_action_logs_action_idx on user_action_logs(action, created_at desc);
