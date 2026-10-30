// Workspace detail routes live in the command palette; the sidebar has five destinations.
async function openDestination(page, destination) {
    await page.getByTestId('brand').focus();
    await page.keyboard.press('ControlOrMeta+k');
    const palette = page.getByTestId('palette');
    await palette.waitFor();
    await palette.locator(`[data-destination="${destination}"]`).click();
}

async function openWorkspace(page, id) {
    await openDestination(page, `ws:${id}`);
    await page.locator(`iframe[data-workspace="${id}"]`).waitFor({ state: 'visible' });
}
module.exports = { openWorkspace, openDestination };
