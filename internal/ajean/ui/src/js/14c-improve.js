// ===== Améliorer le prompt : réécrit le brouillon du composeur avec le modèle chargé =====
// Entrée du menu « + ». Rien n'est envoyé : le texte réécrit remplace celui du champ,
// l'utilisateur le relit, et l'original reste récupérable tant que le champ n'a pas bougé.
let IMPROVE_BACKUP = null;   // {original, improved}
let IMPROVING = false;
const IMPROVE_TIMEOUT_MS = 130000; // un peu plus que le délai du serveur (120 s)

async function improvePrompt(){
  const ta = document.getElementById('input');
  const text = ta.value.trim();
  if(!text){ toast(t('chat.improve_empty')); return; }
  if(IMPROVING) return;
  IMPROVING = true;
  ta.readOnly = true;
  ta.style.opacity = '.5';
  toast(t('chat.improve_busy'));
  const ac = new AbortController();
  const timer = setTimeout(()=>ac.abort(), IMPROVE_TIMEOUT_MS);
  try{
    const r = await jfetch('/api/prompt/improve', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({text}), signal:ac.signal});
    const j = await r.json().catch(()=>({}));
    if(!r.ok || !j.ok){ toast(j.error || t('chat.improve_failed')); return; }
    IMPROVE_BACKUP = {original: ta.value, improved: j.text};
    ta.value = j.text;
    autoGrow(ta); syncSendBtn();
  }catch(e){
    toast(t('common.error_prefix') + (e && e.name === 'AbortError' ? t('api.timeout_error') : (e.message || e)));
  }finally{
    clearTimeout(timer);
    IMPROVING = false;
    ta.readOnly = false;
    ta.style.opacity = '';
    ta.focus();
  }
}

// Le retour n'est proposé que si le champ contient encore EXACTEMENT le texte réécrit :
// dès que l'utilisateur le modifie ou l'envoie, l'original n'a plus de sens à rétablir.
function canRestoreImprovedPrompt(){
  return !!IMPROVE_BACKUP && document.getElementById('input').value === IMPROVE_BACKUP.improved;
}
function restoreImprovedPrompt(){
  if(!canRestoreImprovedPrompt()) return;
  const ta = document.getElementById('input');
  ta.value = IMPROVE_BACKUP.original;
  IMPROVE_BACKUP = null;
  autoGrow(ta); syncSendBtn(); ta.focus();
}
