import { Hono } from 'hono'
import { recordingRouter } from './routes/recording.js'
import { fsRouter } from './routes/fs.js'
import { screenshotRouter } from './routes/screenshot.js'
import { streamRouter } from './routes/stream.js'
import { inputRouter } from './routes/input.js'
import { processRouter } from './routes/process.js'
import { networkRouter } from './routes/network.js'
import { busRouter } from './routes/bus.js'
import { logsRouter } from './routes/logs.js'
import { clipboardRouter } from './routes/clipboard.js'
import { metricsRouter } from './routes/metrics.js'
import { macrosRouter } from './routes/macros.js'
import { scriptsRouter } from './routes/scripts.js'
import { osRouter } from './routes/os.js'
import { browserRouter } from './routes/browser.js'
import { pipeRouter } from './routes/pipe.js'
import { healthRouter } from './routes/health.js'
import { browserExtRouter } from './routes/browser-ext.js'

export const app = new Hono()

app.route('/', recordingRouter)
app.route('/', fsRouter)
app.route('/', screenshotRouter)
app.route('/', streamRouter)
app.route('/', inputRouter)
app.route('/', processRouter)
app.route('/', networkRouter)
app.route('/', busRouter)
app.route('/', logsRouter)
app.route('/', clipboardRouter)
app.route('/', metricsRouter)
app.route('/', macrosRouter)
app.route('/', scriptsRouter)
app.route('/', osRouter)
app.route('/', browserRouter)
app.route('/', pipeRouter)
app.route('/', healthRouter)
app.route('/', browserExtRouter)