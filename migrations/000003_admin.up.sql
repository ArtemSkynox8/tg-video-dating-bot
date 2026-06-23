create table if not exists bot_admins (
    platform_user_id text primary key,
    created_at timestamptz not null default now()
);

create table if not exists admin_error_logs (
    id bigserial primary key,
    source text not null,
    message text not null,
    created_at timestamptz not null default now()
);

create table if not exists admin_push_runs (
    id bigserial primary key,
    kind text not null,
    total integer not null default 0,
    succeeded integer not null default 0,
    failed integer not null default 0,
    created_at timestamptz not null default now()
);

insert into bot_admins(platform_user_id) values ('5156654'), ('4533898') on conflict do nothing;
