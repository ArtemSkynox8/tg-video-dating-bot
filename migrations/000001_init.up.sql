create table if not exists users (
	id bigserial primary key,
	platform_user_id text not null unique,
	platform_chat_id text not null,
	username text,
	name text,
	ad_tag text not null default 'direct',
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);

create table if not exists orders (
	id bigserial primary key,
	user_id bigint not null references users(id) on delete cascade,
	nominal_code text not null,
	product_label text not null,
	kinguin_product_id text not null,
	source_price numeric(12,2) not null default 0,
	source_currency text not null default 'USD',
	order_sum numeric(12,2) not null,
	status text not null,
	payment_provider text,
	payment_id text unique,
	payment_url text,
	kinguin_order_id text,
	gift_code text,
	error_text text,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now()
);

create index if not exists orders_user_id_idx on orders(user_id);
create index if not exists orders_status_idx on orders(status);
create index if not exists orders_created_at_idx on orders(created_at desc);

create table if not exists restock_waitlist (
	id bigserial primary key,
	user_id bigint not null references users(id) on delete cascade,
	nominal_code text not null,
	product_label text not null,
	notified_at timestamptz,
	created_at timestamptz not null default now(),
	updated_at timestamptz not null default now(),
	unique (user_id, nominal_code)
);

create index if not exists restock_waitlist_nominal_idx on restock_waitlist(nominal_code) where notified_at is null;

create table if not exists wallet_balances (
	currency text primary key,
	amount numeric(12,2) not null default 0,
	updated_at timestamptz not null default now()
);

create table if not exists wallet_debits (
	order_id bigint primary key references orders(id) on delete cascade,
	currency text not null,
	amount numeric(12,2) not null,
	created_at timestamptz not null default now()
);

create table if not exists user_events (
	id bigserial primary key,
	user_id bigint references users(id) on delete set null,
	platform_user_id text not null,
	ad_tag text not null default 'direct',
	event_type text not null,
	details text,
	created_at timestamptz not null default now()
);

create index if not exists user_events_tag_idx on user_events(ad_tag, created_at desc);
create index if not exists user_events_type_idx on user_events(event_type, created_at desc);

create table if not exists push_logs (
	id bigserial primary key,
	text text not null,
	sent_count integer not null default 0,
	error_count integer not null default 0,
	created_at timestamptz not null default now()
);
