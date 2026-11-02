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
    const names = (await fs.readdir(directory)).filter(name => name.endsWith('.ansi')).map(name => name.slice(0, -5))
    for (const name of names) {
      const terminal = new Terminal({ cols: 120, rows: 30, allowProposedApi: true })
      const recording = await fs.readFile(path.join(directory, `${name}.ansi`), 'utf8')
      await new Promise((resolve) => terminal.write(recording, resolve))
      const lines = [], styledRows = []
      for (let row = 0; row < terminal.rows; row++) {
        const line = terminal.buffer.active.getLine(row)
        lines.push(line?.translateToString(true) ?? '')
        const runs = []
        for (let column = 0; column < terminal.cols; column++) {
          const cell = line?.getCell(column)
          if (!cell || cell.getWidth() === 0) continue
          const style = []
          if (cell.isFgRGB()) style.push(`color:#${cell.getFgColor().toString(16).padStart(6, '0')}`)
          if (cell.isBgRGB()) style.push(`background:#${cell.getBgColor().toString(16).padStart(6, '0')}`)
          if (cell.isBold()) style.push('font-weight:700')
          if (cell.isItalic()) style.push('font-style:italic')
          if (cell.isUnderline()) style.push('text-decoration:underline;text-underline-offset:3px')
          if (cell.isDim()) style.push('opacity:.65')
          const css = style.join(';'), text = cell.getChars() || ' '
          if (runs.length && runs.at(-1).css === css) runs.at(-1).text += text
          else runs.push({ css, text })
        }
        styledRows.push(runs)
      }
      const screen = lines.join('\n')
      if (!screen.includes('spaniel - ') || !screen.includes('Ctrl+C quit')) {
        throw new Error(`${name} did not capture the actual watch screen`)
      }
      if (name === 'watch-reconnecting' && !screen.includes('reconnecting:')) throw new Error('missing real reconnect state')
      const page = await browser.newPage({ viewport: { width: 1320, height: 720 }, deviceScaleFactor: 1 })
      await page.setContent('<!doctype html><meta charset="utf-8"><style>body{margin:0;background:#11151b;color:#d8dee9}pre{margin:20px;font:17px/22px "DejaVu Sans Mono",Consolas,monospace;white-space:pre}</style><pre id="terminal"></pre>')
      await page.locator('#terminal').evaluate((element, rows) => {
        for (const runs of rows) {
          for (const run of runs) {
            const span = document.createElement('span')
            span.setAttribute('style', run.css)
            span.textContent = run.text
            element.append(span)
          }
          element.append(document.createTextNode('\n'))
        }
      }, styledRows)
      await page.screenshot({ path: path.join(directory, `${name}.png`), fullPage: true })
      await page.close()
      terminal.dispose()
    }
  } finally {
    await browser.close()
  }
}
main().catch((error) => { console.error(error); process.exitCode = 1 })
