create table if not exists users (
	id bigserial primary key,
	platform_user_id text not null unique,
	platform_chat_id text not null,
	username text,
	name text,
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
