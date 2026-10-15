#!/usr/bin/env node
// 机检「源码路由 ↔ docs/API.md」一致性：改了路由不改文档 = 未完成。
//
// 约定：路由在 internal/proxy/routes.go 的 routeTable 里以 `{method: "GET", path: "/*"}`
//      字面量登记，docs/API.md 用 `### METHOD /path` 作为每个端点的标题。
//   - 同一路径登记多个动词时，每个「方法 路径」都要各自有文档标题。
//
// 退出码：0 = 一致；1 = 有路由缺文档或文档多出路由。

import { readFileSync, readdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'

const root = join(dirname(fileURLToPath(import.meta.url)), '..')

/** 从 internal/proxy 下所有 .go 抽出登记的具体路由。 */
function collectCodeRoutes() {
  const dir = join(root, 'internal', 'proxy')
  const routes = new Set()
  for (const name of readdirSync(dir)) {
    if (!name.endsWith('.go') || name.endsWith('_test.go')) continue
    const src = readFileSync(join(dir, name), 'utf8')
    const re = /\{\s*method:\s*"([A-Z]+)"\s*,\s*path:\s*"(\S+)"\s*\}/g
    let m
    while ((m = re.exec(src)) !== null) {
      routes.add(`${m[1]} ${m[2]}`)
    }
  }
  return routes
}

/** 从 docs/API.md 抽出文档化的端点。 */
function collectDocRoutes() {
  const src = readFileSync(join(root, 'docs', 'API.md'), 'utf8')
  const routes = new Set()
  const re = /^###\s+([A-Z]+)\s+(\S+)/gm
  let m
  while ((m = re.exec(src)) !== null) {
    routes.add(`${m[1]} ${m[2]}`)
  }
  return routes
}

const code = collectCodeRoutes()
const docs = collectDocRoutes()

const undocumented = [...code].filter((r) => !docs.has(r)).sort()
const orphaned = [...docs].filter((r) => !code.has(r)).sort()

console.log(`源码 API 路由：${code.size} 条`)
for (const r of [...code].sort()) console.log(`  ${r}`)
console.log(`文档端点：${docs.size} 条`)

let failed = false
if (undocumented.length) {
  failed = true
  console.error('\n以下路由已实现但未写入 docs/API.md：')
  for (const r of undocumented) console.error(`  - ${r}`)
}
if (orphaned.length) {
  failed = true
  console.error('\n以下端点写入文档但源码中不存在：')
  for (const r of orphaned) console.error(`  - ${r}`)
}

if (failed) {
  console.error('\n检查未通过')
  process.exit(1)
}
console.log('\n检查通过：源码路由与 docs/API.md 一致')
