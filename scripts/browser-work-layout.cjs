const assert = require('node:assert/strict');
const path = require('node:path');
module.exports = async function checkWorkLayout(page, _count, artifacts, name) {
 for (const [size,width,height] of [['desktop',1440,1000],['phone',390,844]]) {
  await page.setViewportSize({width,height});
  for (const title of ['Now','Next','Later']) await page.getByRole('heading',{name:title,exact:true}).waitFor();
  assert.equal(await page.locator('[data-board-column],[data-drag-handle]').count(),0);
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth+2),false);
  if(!process.env.BROWSER_SKIP_SCREENSHOTS) await page.screenshot({path:path.join(artifacts,`${name}-work-${size}.png`)});
 }
 await page.setViewportSize({width:1440,height:1000});
};
