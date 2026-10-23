import type { Plugin } from 'vite'
import { event } from '../src/fixtures.ts'
import { scenario } from './scenarios.ts'

function sendJSON(res: { writeHead(status: number, headers: Record<string, string>): void; end(body?: string): void }, status: number, body: unknown) {
  res.writeHead(status, { 'content-type': 'application/json' })
  res.end(JSON.stringify(body))
}

export function mockLocalApi(): Plugin {
  return {
    name: 'agent-marketplace-mock-local-api',
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const path = new URL(req.url ?? '/', 'http://localhost').pathname
        if (!path.startsWith('/api/')) {
          next()
          return
        }
        const current = scenario(process.env.GUI_PREVIEW_SCENARIO ?? 'default')
        switch (path) {
          case '/api/snapshot':
            sendJSON(res, 200, current.snapshot)
            return
          case '/api/configuration':
            sendJSON(res, 200, current.configuration)
            return
          case '/api/events':
            res.writeHead(200, {
              'content-type': 'text/event-stream',
              'cache-control': 'no-cache',
              connection: 'keep-alive',
            })
            res.write(`data: ${JSON.stringify(event({ sequence: 1, generation: 1, payload: current.snapshot }))}\n\n`)
            const operation = current.operation
            const frames = [
              ...(operation === undefined ? [] : [
                event({ sequence: 2, generation: 2, operation, payload: current.snapshot }),
                event({ sequence: 3, generation: 2, operation, type: 'progress', payload: { progressSequence: 1, phase: current.phase ?? '' } }),
              ]),
              ...(current.notifications ?? []).map((notification, index) =>
                event({ sequence: 4 + index, generation: 2, operation, type: 'notification', payload: notification }),
              ),
            ]
            for (const frame of frames) {
              res.write(`data: ${JSON.stringify(frame)}\n\n`)
            }
            return
          case '/api/release':
            sendJSON(res, 200, { latestVersion: '' })
            return
          case '/api/telemetry':
            res.writeHead(204)
            res.end()
            return
          default:
            sendJSON(res, 501, { code: 'preview.unsupported', message: `preview has no fixture for ${path}` })
        }
      })
    },
    transformIndexHtml(html) {
      return html.replace(
        '</head>',
        "    <script>window.__AM_GUI_BOOTSTRAP__ = { credential: 'preview', version: 'preview' }</script>\n  </head>",
      )
    },
  }
}
