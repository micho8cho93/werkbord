import {createHandler} from "./handler.ts";
Deno.serve(createHandler({
 project:Deno.env.get("SUPABASE_URL")!,
 publishable:Deno.env.get("SUPABASE_ANON_KEY")!,
 secret:Deno.env.get("SUPABASE_SERVICE_ROLE_KEY")!,
 signingKey:Deno.env.get("WERKBORD_LICENSE_SIGNING_PKCS8"),
 allowedOrigins:(Deno.env.get("WERKBORD_LICENSING_ORIGINS")||"https://micho8cho93.github.io").split(","),
}));
