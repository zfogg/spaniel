// Rasterize actual seeded Bubble Tea renderer recordings. No feature UI is
// recreated here: xterm interprets the captured ANSI cursor/erase sequences.
// Usage: node scripts/capture-watch.cjs CAPTURE_DIR TOOLS_NODE_MODULES
const fs = require('node:fs/promises')
const path = require('node:path')
const { Terminal } = require(path.resolve(process.argv[3], '@xterm/headless'))
const { chromium } = require(path.resolve('frontend/node_modules/@playwright/test'))

async function main() {
  const directory = path.resolve(process.argv[2])
  const browser = await chromium.launch({ headless: true })
  try {
    for (const name of ['watch-traces', 'watch-spans', 'watch-reconnecting']) {
      const terminal = new Terminal({ cols: 120, rows: 30, allowProposedApi: true })
      const recording = await fs.readFile(path.join(directory, `${name}.ansi`), 'utf8')
      await new Promise((resolve) => terminal.write(recording, resolve))
      const lines = []
      for (let row = 0; row < terminal.rows; row++) {
        lines.push(terminal.buffer.active.getLine(row)?.translateToString(true) ?? '')
      }
      const screen = lines.join('\n')
      if (!screen.includes('spaniel - ') || !screen.includes('Foundation extension point') || !screen.includes('q quit')) {
        throw new Error(`${name} did not capture the actual foundation screen`)
      }
      if (name === 'watch-reconnecting' && !screen.includes('reconnecting:')) throw new Error('missing real reconnect state')
      const page = await browser.newPage({ viewport: { width: 1320, height: 720 }, deviceScaleFactor: 1 })
      await page.setContent('<!doctype html><meta charset="utf-8"><style>body{margin:0;background:#11151b;color:#d8dee9}pre{margin:20px;font:17px/22px "DejaVu Sans Mono",Consolas,monospace;white-space:pre}</style><pre id="terminal"></pre>')
      await page.locator('#terminal').evaluate((element, text) => { element.textContent = text }, screen)
      await page.screenshot({ path: path.join(directory, `${name}.png`), fullPage: true })
      await page.close()
      terminal.dispose()
    }
  } finally {
    await browser.close()
  }
}
main().catch((error) => { console.error(error); process.exitCode = 1 })
