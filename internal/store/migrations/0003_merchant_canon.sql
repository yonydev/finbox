-- merchant_canon is the name the user sees; transactions.merchant keeps the raw
-- receipt text forever. Default '' rather than a plain not-null: migrate runs
-- while the previous container is still confirming receipts, and those inserts
-- do not know the column yet. Every select coalesces '' back to the raw name,
-- and `finbox rerule` right after migrate replaces the copy below with the
-- normalized name.
alter table transactions add column merchant_canon text not null default '';
update transactions set merchant_canon = merchant;
