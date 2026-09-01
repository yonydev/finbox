create table receipts (
  id                 uuid primary key default gen_random_uuid(),
  blob_key           text not null,
  blob_sha256        text not null unique,
  status             text not null check (status in ('pending','awaiting_confirm','confirmed','discarded','failed')),
  fail_reason        text,
  extraction         jsonb,
  model              text,
  tg_message_id      bigint unique,
  tg_chat_id         bigint,
  tg_card_message_id bigint,
  created_at         timestamptz not null default now(),
  updated_at         timestamptz not null default now()
);
create index receipts_needing_attention on receipts(status)
  where status in ('awaiting_confirm','failed');

create table transactions (
  id           uuid primary key default gen_random_uuid(),
  receipt_id   uuid references receipts,
  occurred_on  date not null,
  merchant     text not null,
  amount_minor bigint not null check (amount_minor <> 0),  -- negative = refund/credit
  currency     text not null default 'MXN' check (currency ~ '^[A-Z]{3}$'),
  source       text not null check (source in ('receipt','manual','csv')),
  voided_at    timestamptz,
  created_at   timestamptz not null default now(),
  updated_at   timestamptz not null default now()
);
create unique index one_active_txn_per_receipt
  on transactions(receipt_id) where receipt_id is not null and voided_at is null;
create index txn_by_date on transactions(occurred_on desc) where voided_at is null;

create table transaction_items (
  id             uuid primary key default gen_random_uuid(),
  transaction_id uuid not null references transactions,
  position       int not null,
  name           text not null,
  quantity_milli bigint,
  amount_minor   bigint check (amount_minor <> 0)   -- line total; null = unpriced line
);

create table edit_log (
  id             uuid primary key default gen_random_uuid(),
  transaction_id uuid not null references transactions,
  field          text not null,
  old_value      text,        -- canonical form (amounts in minor units)
  new_value      text,
  created_at     timestamptz not null default now()
);
create index edit_log_by_txn on edit_log(transaction_id);

create table processed_updates (   -- COMPLETION records for Telegram updates (see §4)
  update_id    bigint primary key,
  completed_at timestamptz,        -- null = claimed, still in flight
  created_at   timestamptz not null default now()
);
-- boot sweep purges rows older than 7 days (Telegram redelivers within ~24h)
