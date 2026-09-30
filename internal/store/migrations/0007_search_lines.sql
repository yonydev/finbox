-- one model-written line per expense (synonyms, brand → product) appended to the search doc.
-- doc_hash is the cache key (prompt + expense text): the line is redone only when either changes.
-- model is provenance only, never compared.
create table search_lines (
  transaction_id uuid primary key references transactions,
  doc_hash       text not null,
  line           text not null,
  model          text not null,
  created_at     timestamptz not null default now()
);
