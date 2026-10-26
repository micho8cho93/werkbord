-- Run against the vendor operator project after the migration, never a customer database.
-- The transaction rolls back; no operator account, key or entitlement is created.
begin;
do $$
begin
  if has_table_privilege('anon','premium_private.keys','select')
    or has_table_privilege('authenticated','premium_private.keys','select')
    or has_table_privilege('authenticated','premium_private.keys','insert')
    or has_table_privilege('authenticated','premium_private.keys','update') then
    raise exception 'Direct client register access';
  end if;
  if has_function_privilege('anon','public.premium_list()','execute')
    or has_function_privilege('anon','public.premium_prepare(uuid,text,text)','execute')
    or has_function_privilege('anon','public.premium_read(text)','execute')
    or has_function_privilege('authenticated','public.premium_complete(text,uuid,uuid,text,text)','execute') then
    raise exception 'Unexpected public issuance grant';
  end if;
  if not has_function_privilege('service_role','public.premium_complete(text,uuid,uuid,text,text)','execute') then
    raise exception 'Protected completion is unavailable';
  end if;
  if not (select relrowsecurity from pg_class where oid='premium_private.keys'::regclass) then raise exception 'Register RLS missing'; end if;
  if exists(select 1 from pg_proc p join pg_namespace n on n.oid=p.pronamespace where n.nspname='public' and p.proname like 'premium_%' and p.prosecdef) then raise exception 'Public wrapper bypasses invoker security'; end if;
end $$;
set local role authenticated;
do $$
begin
  begin
    perform public.premium_list();
    raise exception 'Unauthenticated register read accepted';
  exception when insufficient_privilege then null;
  end;
  begin
    perform public.premium_prepare(gen_random_uuid(),'No operator','');
    raise exception 'Unauthenticated issuance accepted';
  exception when insufficient_privilege then null;
  end;
end $$;
rollback;
