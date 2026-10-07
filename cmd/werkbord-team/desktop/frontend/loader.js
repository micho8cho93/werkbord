'use strict';
const retry = document.getElementById('retry');
async function connect() {
  retry.hidden = true; document.getElementById('error').hidden = true;
  try { const address = await window.go.main.App.Connect(); window.location.replace(address); }
  catch (e) { document.getElementById('progress').textContent = 'Team needs a moment to connect.'; const error = document.getElementById('error'); error.textContent = String(e); error.hidden = false; retry.hidden = false; }
}
retry.addEventListener('click', connect);
window.addEventListener('load', connect);
