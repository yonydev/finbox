create extension if not exists vector;
create extension if not exists pg_trgm;
-- one vector per expense; doc is the exact text that was embedded (debugging + trigram baseline)
create table transaction_embeddings (
  transaction_id uuid primary key references transactions,
  model          text not null,
  doc_hash       text not null,
  doc            text not null,
  embedding      vector(512) not null,
  created_at     timestamptz not null default now()
);
