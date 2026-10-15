// Shared layout regression: wide boards use lanes; Individual groups rows in narrow windows.
const assert = require('node:assert/strict');
const path = require('node:path');
module.exports = async function checkKanban(page, count, artifacts, name) {
    const board = page.getByRole('region', { name: 'Kanban board', exact: true });
    await board.waitFor();
    for (const [size, width, height] of [['desktop', 1440, 1000], ['window', 1100, 800], ['tablet', 900, 800], ['mobile', 390, 844]]) {
        await page.setViewportSize({ width, height });
        if (name === 'individual' && width < 900) {
            // The phone shows one column at a time behind a segmented control (docs/MOBILE.md); every column stays one touch away.
            const switcher = page.getByRole('group', { name: 'Board column', exact: true });
            await switcher.waitFor();
            const buttons = switcher.getByRole('button');
            assert.equal(await buttons.count(), count, `${name} ${size}: one segment for each column`);
            for (let i = 0; i < count; i++) {
                await buttons.nth(i).click();
                const shown = await board.evaluate(el => [...el.querySelectorAll(':scope > section')].filter(col => getComputedStyle(col).display !== 'none').length);
                assert.equal(shown, 1, `${name} ${size}: exactly one column is shown`);
                assert.equal(await buttons.nth(i).getAttribute('aria-pressed'), 'true', `${name} ${size}: the segment says which`);
            }
            assert.equal(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2), false, `${name} ${size}: no horizontal overflow`);
            await page.screenshot({ path: path.join(artifacts, `${name}-kanban-${size}.png`) });
            continue;
        }
        // Team can replace its DOM during a live update; wait for the resized board to finish laying out.
        await page.waitForFunction(expected => {
            const columns = document.querySelectorAll('[aria-label="Kanban board"] > section');
            return columns.length === expected && [...columns].every(col => col.getBoundingClientRect().height > 30);
        }, count);
        await board.evaluate(el => { el.scrollLeft = 0; });
        const layout = await board.evaluate(el => ({
            columns: [...el.querySelectorAll(':scope > section')].map(col => {
                const r = col.getBoundingClientRect();
                return { x: r.x, y: r.y, width: r.width, height: r.height, display: getComputedStyle(col).display };
            }),
            width: el.clientWidth, scrollWidth: el.scrollWidth,
            containerWidth: el.closest('.view')?.clientWidth ?? innerWidth,
            pageOverflow: document.documentElement.scrollWidth > innerWidth + 2,
        }));
        assert.equal(layout.columns.length, count);
        assert.equal(layout.pageOverflow, false, `${name} ${size}: horizontal scrolling stays inside the board`);
        const grouped = name === 'individual' && layout.containerWidth <= 1055;
        layout.columns.forEach((col, i) => {
            assert.notEqual(col.display, 'none', `${name} ${size}: all columns remain present`);
            assert.ok(col.width > 200 && col.height > (grouped ? 30 : 300), `${name} ${size}: usable column dimensions ${JSON.stringify(col)}`);
            if (grouped) {
                assert.ok(Math.abs(col.x - layout.columns[0].x) < 2, `${name} ${size}: groups align vertically`);
                if (i) assert.ok(col.y > layout.columns[i - 1].y, `${name} ${size}: grouped workflow order`);
            } else {
                assert.ok(Math.abs(col.y - layout.columns[0].y) < 2, `${name} ${size}: lanes stay side by side`);
                if (i) assert.ok(col.x > layout.columns[i - 1].x, `${name} ${size}: lane workflow order`);
            }
        });
        if (!grouped) {
        const scrolling = await board.locator(':scope > section').first().evaluate(col => {
            const list = col.querySelector('.list, .column-cards');
            const top = col.querySelector('h2, h3').getBoundingClientRect().top;
            list.scrollTop = list.scrollHeight;
            const result = { overflow: list.scrollHeight > list.clientHeight, moved: list.scrollTop > 0, headerTop: col.querySelector('h2, h3').getBoundingClientRect().top, top };
            list.scrollTop = 0;
            return result;
        });
        assert.ok(scrolling.overflow && scrolling.moved, `${name} ${size}: long columns scroll internally`);
        assert.equal(scrolling.headerTop, scrolling.top, `${name} ${size}: column heading stays visible`);
        } else {
            assert.ok(layout.scrollWidth <= layout.width + 2, `${name} ${size}: grouped boards need no horizontal pan`);
            await board.locator(':scope > section').last().getByRole('heading').scrollIntoViewIfNeeded();
            const lastHeading = await board.locator(':scope > section').last().getByRole('heading').boundingBox();
            assert(lastHeading && lastHeading.y >= 0 && lastHeading.y < height, `${name} ${size}: the final group remains reachable`);
        }
        if (!grouped && size !== 'desktop') {
            assert.ok(layout.scrollWidth > layout.width, `${name} ${size}: narrow boards pan horizontally`);
            await board.focus();
            await page.keyboard.press('End');
            // Verify the last column is reachable, including on touch layouts.
            await board.locator(':scope > section').last().evaluate(el => el.scrollIntoView({ block: 'nearest', inline: 'nearest' }));
            assert.ok(await board.evaluate(el => el.scrollLeft > 0), `${name} ${size}: later columns can be reached`);
            await board.evaluate(el => { el.scrollLeft = 0; });
        }
        if (!process.env.BROWSER_SKIP_SCREENSHOTS && size !== 'tablet') {
            await page.screenshot({ path: path.join(artifacts, `${name}-kanban-${size}.png`) });
        }
    }
    await page.setViewportSize({ width: 1100, height: 800 });
    await page.evaluate(() => { if (window.teamTheme) window.teamTheme.set('dark'); else document.documentElement.dataset.theme = 'dark'; });
    if (!process.env.BROWSER_SKIP_SCREENSHOTS) await page.screenshot({ path: path.join(artifacts, `${name}-kanban-dark.png`) });
    await page.evaluate(() => { if (window.teamTheme) window.teamTheme.set('light'); else delete document.documentElement.dataset.theme; });
    await page.setViewportSize({ width: 1440, height: 1000 });
    console.log(`PASS ${name} responsive board at desktop, small-window, tablet and phone widths`);
};
