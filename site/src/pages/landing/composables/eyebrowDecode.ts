const POOL = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ'
const DURATION = 1100
const LOCK_START = 0.25
const FRAME_INTERVAL = 1000 / 36
const STAGGER = 80
const VISIBLE_THRESHOLD = 0.6

type Decoder = { text: string; onFrame: (value: string) => void; onDone: () => void }

const decoders = new Map<Element, Decoder>()
const running = new Map<Element, () => void>()
let observer: IntersectionObserver | undefined

function prefersReducedMotion() {
  return matchMedia('(prefers-reduced-motion: reduce)').matches
}

function run(decoder: Decoder, delay: number) {
  const chars = [...decoder.text]
  const letters = chars.flatMap((char, index) => (/\S/.test(char) ? [index] : []))
  let start: number | undefined
  let last = Number.NEGATIVE_INFINITY

  const frame = (now: number) => {
    start ??= now + delay
    const elapsed = now - start
    if (prefersReducedMotion() || elapsed >= DURATION) return decoder.onDone()
    if (elapsed >= 0 && now - last >= FRAME_INTERVAL) {
      last = now
      const out = [...chars]
      letters.forEach((index, order) => {
        const lockAt = DURATION * (LOCK_START + ((1 - LOCK_START) * (order + 1)) / letters.length)
        if (elapsed < lockAt) out[index] = POOL[Math.floor(Math.random() * POOL.length)]
      })
      decoder.onFrame(out.join(''))
    }
    handle = requestAnimationFrame(frame)
  }

  const scramble = [...chars]
  letters.forEach((index) => (scramble[index] = POOL[Math.floor(Math.random() * POOL.length)]))
  decoder.onFrame(scramble.join(''))

  let handle = requestAnimationFrame(frame)
  return () => cancelAnimationFrame(handle)
}

/**
 * Resolves a short label from random letters to its real text once, the
 * first time it is mostly visible. Labels entering together start 80ms apart.
 * Returns a cleanup function that cancels observation and animation.
 */
export function observeDecode(element: Element, decoder: Decoder) {
  if (typeof IntersectionObserver === 'undefined') return () => undefined

  observer ??= new IntersectionObserver(
    (entries) => {
      let order = 0
      for (const entry of entries) {
        if (!entry.isIntersecting) continue
        observer?.unobserve(entry.target)
        const target = decoders.get(entry.target)
        decoders.delete(entry.target)
        if (!target || prefersReducedMotion()) continue
        running.set(entry.target, run(target, order++ * STAGGER))
      }
    },
    { threshold: VISIBLE_THRESHOLD },
  )

  decoders.set(element, decoder)
  observer.observe(element)

  return () => {
    observer?.unobserve(element)
    decoders.delete(element)
    running.get(element)?.()
    running.delete(element)
  }
}
