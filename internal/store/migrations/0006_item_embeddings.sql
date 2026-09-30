-- one vector per receipt line: the expense vector dilutes items on long tickets (eval 2026-09-29)
create table item_embeddings (
  item_id        uuid primary key references transaction_items,
  transaction_id uuid not null references transactions,
  model          text not null,
  doc_hash       text not null,
  doc            text not null,
  embedding      vector(512) not null,
  created_at     timestamptz not null default now()
);
