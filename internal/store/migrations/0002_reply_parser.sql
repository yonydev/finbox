-- raw model output, copied once on the first reply-correction; input for finbox eval
alter table receipts add column extraction_raw jsonb;
-- who made the edit: existing rows were all CLI
alter table edit_log add column source text not null default 'cli' check (source in ('cli','reply','wizard'));
