// Shown in place of the app when it cannot be reached. A computer that was asleep or a busy service usually fixes
// itself, so this tries again, slowly and only while it is on screen. A signed-out phone needs a new code, not retries.
const reason = document.body.dataset.reason;
const reload = () => location.reload();
document.getElementById("retry").addEventListener("click", reload);
if (reason === "offline" || reason === "unavailable") {
  let wait = 10_000;
  const tick = () => {
    if (document.visibilityState === "visible") return reload();
    setTimeout(tick, wait);
  };
  setTimeout(tick, wait);
}
