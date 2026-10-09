// ===== Interrupteur des hooks UserPromptSubmit (voir hooks_prompt.go) =====
// Le serveur lance les hooks UserPromptSubmit de hooks.json à chaque demande, mais
// seulement quand cet interrupteur est allumé (coupé par défaut). Les hooks agissent
// hors de l'interface (toast, journal) : rien à afficher ici, juste l'état du réglage.
let PROMPT_HOOK_ON = false;

async function loadPromptHook(){
  try{
    const r = await jfetch('/api/prompthook');
    const j = await r.json().catch(()=>({}));
    if(j.ok) PROMPT_HOOK_ON = !!j.on;
  }catch(e){ /* interrupteur indisponible : il reste coupé */ }
}

async function togglePromptHook(){
  const want = !PROMPT_HOOK_ON;
  try{
    const r = await jfetch('/api/prompthook', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({on:want})});
    const j = await r.json().catch(()=>({}));
    if(!r.ok || !j.ok){ toast(j.error || t('common.error_prefix')); return; }
    PROMPT_HOOK_ON = !!j.on;
    toast(t(PROMPT_HOOK_ON ? 'advice.on' : 'advice.off'));
  }catch(e){ toast(t('common.error_prefix') + (e.message || e)); }
}

loadPromptHook();
