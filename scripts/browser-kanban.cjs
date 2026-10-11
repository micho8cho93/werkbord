// Shared layout regression: the workflow must remain a Kanban board in small app windows and on phones.
const assert = require('node:assert/strict');
const path = require('node:path');
module.exports = async function checkKanban(page, count, artifacts, name) {
    const board = page.getByRole('region', { name: 'Kanban board', exact: true });
    await board.waitFor();
    for (const [size, width, height] of [['desktop', 1440, 1000], ['window', 1100, 800], ['tablet', 900, 800], ['mobile', 390, 844]]) {
        await page.setViewportSize({ width, height });
        // Team can replace its DOM during a live update; wait for the resized board to finish laying out.
        await page.waitForFunction(expected => {
            const columns = document.querySelectorAll('[aria-label="Kanban board"] > section');
            return columns.length === expected && [...columns].every(col => col.getBoundingClientRect().height > 300);
        }, count);
        await board.evaluate(el => { el.scrollLeft = 0; });
        const layout = await board.evaluate(el => ({
            columns: [...el.querySelectorAll(':scope > section')].map(col => {
                const r = col.getBoundingClientRect();
                return { x: r.x, y: r.y, width: r.width, height: r.height, display: getComputedStyle(col).display };
            }),
            width: el.clientWidth, scrollWidth: el.scrollWidth,
            pageOverflow: document.documentElement.scrollWidth > innerWidth + 2,
        }));
        assert.equal(layout.columns.length, count);
        assert.equal(layout.pageOverflow, false, `${name} ${size}: horizontal scrolling stays inside the board`);
        layout.columns.forEach((col, i) => {
            assert.notEqual(col.display, 'none', `${name} ${size}: all columns remain present`);
            assert.ok(col.width > 200 && col.height > 300, `${name} ${size}: usable column dimensions ${JSON.stringify(col)}`);
            assert.ok(Math.abs(col.y - layout.columns[0].y) < 2, `${name} ${size}: columns stay side by side`);
            if (i) assert.ok(col.x > layout.columns[i - 1].x, `${name} ${size}: workflow order`);
        });
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
        if (size !== 'desktop') {
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
    console.log(`PASS ${name} Kanban columns at desktop, small-window, tablet and phone widths`);
};
