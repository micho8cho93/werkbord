-- Vendor administration only; never a dependency of a customer app or Team host.
-- Reuses the existing hosted console's operator allow-list and live MFA guard.
create schema if not exists premium_private;
revoke all on schema premium_private from public,anon,authenticated;
grant usage on schema premium_private to authenticated,service_role;
create table premium_private.keys (
  id text primary key default ('pk_' || replace(gen_random_uuid()::text,'-','')),
  request_id uuid not null unique,
  holder text not null check (octet_length(holder) between 1 and 80 and holder !~ '[[:cntrl:]]'),
  order_reference text not null default '' check (length(order_reference)<=256),
  issued_at timestamptz not null default date_trunc('second',now()),
  actor uuid not null references auth.users(id),
  unlock_key text check (unlock_key is null or (unlock_key like 'wbpremium-%' and length(unlock_key)<=1024)),
  issuer_public_key text check (issuer_public_key is null or issuer_public_key ~ '^[A-Za-z0-9_-]{43}$'),
  completed_at timestamptz,
  check ((unlock_key is null) = (completed_at is null)),
  check ((unlock_key is null) = (issuer_public_key is null))
);
alter table premium_private.keys enable row level security;
revoke all on premium_private.keys from public,anon,authenticated;

create function premium_private.list_keys() returns jsonb language plpgsql security definer set search_path='' as $$
begin
  perform licensing_private.operator_id();
  return coalesce((select jsonb_agg(to_jsonb(k)-'unlock_key' order by issued_at desc) from premium_private.keys k),'[]'::jsonb);
end $$;
create function premium_private.prepare(p_request uuid,p_holder text,p_order text) returns jsonb language plpgsql security definer set search_path='' as $$
declare actor_id uuid := licensing_private.operator_id(); k premium_private.keys;
begin
  if p_request is null or p_holder is null or octet_length(btrim(p_holder)) not between 1 and 80
    or p_holder ~ '[[:cntrl:]]' or p_order is null or length(p_order)>256 then raise exception 'Enter a holder (up to 80 UTF-8 bytes) and an order reference up to 256 characters'; end if;
  perform pg_advisory_xact_lock(hashtextextended(p_request::text,0));
  select * into k from premium_private.keys where request_id=p_request;
  if found then
    if k.holder<>btrim(p_holder) or k.order_reference<>p_order then raise exception 'This request already has different details; refresh the register'; end if;
  else
    insert into premium_private.keys(request_id,holder,order_reference,actor) values(p_request,btrim(p_holder),p_order,actor_id) returning * into k;
  end if;
  return to_jsonb(k);
end $$;
create function premium_private.read_key(p_id text) returns jsonb language plpgsql security definer set search_path='' as $$
begin
  perform licensing_private.operator_id();
  return (select to_jsonb(k) from premium_private.keys k where id=p_id);
end $$;
create function premium_private.complete(p_id text,p_actor uuid,p_session uuid,p_key text,p_issuer text) returns jsonb language plpgsql security definer set search_path='' as $$
declare k premium_private.keys;
begin
  if current_setting('role',true)<>'service_role' then raise exception 'Service authorisation required' using errcode='42501'; end if;
  perform licensing_private.operator_id(p_actor,p_session);
  select * into k from premium_private.keys where id=p_id for update;
  if not found then raise exception 'No prepared Premium key'; end if;
  if k.unlock_key is not null then
    if k.unlock_key<>p_key or k.issuer_public_key<>p_issuer then raise exception 'This request already has a different signed result'; end if;
  else
    update premium_private.keys set unlock_key=p_key,issuer_public_key=p_issuer,completed_at=now() where id=p_id returning * into k;
  end if;
  return to_jsonb(k);
end $$;
revoke all on all functions in schema premium_private from public,anon,authenticated;
grant execute on function premium_private.list_keys(),premium_private.prepare(uuid,text,text),premium_private.read_key(text) to authenticated;
grant execute on function premium_private.complete(text,uuid,uuid,text,text) to service_role;

create function public.premium_list() returns jsonb language sql security invoker set search_path='' as $$ select premium_private.list_keys() $$;
create function public.premium_prepare(p_request uuid,p_holder text,p_order text) returns jsonb language sql security invoker set search_path='' as $$ select premium_private.prepare(p_request,p_holder,p_order) $$;
create function public.premium_read(p_id text) returns jsonb language sql security invoker set search_path='' as $$ select premium_private.read_key(p_id) $$;
create function public.premium_complete(p_id text,p_actor uuid,p_session uuid,p_key text,p_issuer text) returns jsonb language sql security invoker set search_path='' as $$ select premium_private.complete(p_id,p_actor,p_session,p_key,p_issuer) $$;
revoke all on function public.premium_list(),public.premium_prepare(uuid,text,text),public.premium_read(text),public.premium_complete(text,uuid,uuid,text,text) from public,anon,authenticated;
grant execute on function public.premium_list(),public.premium_prepare(uuid,text,text),public.premium_read(text) to authenticated;
grant execute on function public.premium_complete(text,uuid,uuid,text,text) to service_role;
