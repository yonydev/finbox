-- human-set for now; category_source distinguishes it from the LLM/rule
-- categories that later steps add, so their error rates can be read apart
alter table transactions add column category text;
alter table transactions add column category_source text
  check (category_source in ('llm','rule','human'));
alter table transactions add constraint category_pair
  check ((category is null) = (category_source is null));
