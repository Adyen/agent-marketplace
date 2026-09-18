<script setup lang="ts">
import { computed } from 'vue'
import { auditVerdictColor, formatDuration, resourceRoute, resourceSourceRangeRoute } from '~/lib/presentation'
import type { AssessmentFinding, Audit, ResourceKind } from '~/types/marketplace'

type Props = {
  audit: Audit
  registry: string
  kind: ResourceKind
  name: string
  revision: string
}

const props = defineProps<Props>()

const revisionRoute = computed(() => resourceRoute({ registry_id: props.registry, kind: props.kind, name: props.name, revision: props.revision, content_digest: props.revision }))

function findingLocationRoute(finding: AssessmentFinding): string {
  if (!finding.location?.path) return ''
  return resourceSourceRangeRoute(props.registry, props.kind, props.name, props.revision, {
    path: finding.location.path,
    line: finding.location.line,
    column: finding.location.column,
    start_byte: finding.location.start_byte,
    end_byte: finding.location.end_byte,
  })
}
</script>

<template>
  <article class="border border-default bg-default p-5 sm:p-7">
    <div class="flex flex-wrap items-start justify-between gap-4 border-b border-default pb-5">
      <div class="min-w-0">
        <p class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">Analyzer audit</p>
        <h1 class="mt-1 break-words font-mono text-2xl tracking-tight">
          <span class="text-muted">{{ audit.analyzer_id }}</span>
          <span class="text-highlighted">@{{ audit.analyzer_version }}</span>
        </h1>
        <p class="mt-2 text-sm leading-6 text-muted">{{ audit.summary }}</p>
      </div>
      <UBadge :color="auditVerdictColor(audit.verdict)" variant="subtle" class="shrink-0">{{ audit.verdict }}</UBadge>
    </div>

    <dl class="grid gap-x-8 gap-y-4 border-b border-default py-5 sm:grid-cols-2">
      <div>
        <dt class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">Role</dt>
        <dd class="mt-1 font-mono text-sm text-highlighted">{{ audit.role }}</dd>
      </div>
      <div>
        <dt class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">Result digest</dt>
        <dd class="mt-1 break-all font-mono text-sm text-highlighted">{{ audit.result_digest }}</dd>
      </div>
      <div>
        <dt class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">Finding count</dt>
        <dd class="mt-1 font-mono text-sm text-highlighted">{{ audit.finding_count }} finding{{ audit.finding_count === 1 ? '' : 's' }}</dd>
      </div>
      <div>
        <dt class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">Duration</dt>
        <dd class="mt-1 font-mono text-sm text-highlighted">{{ formatDuration(audit.duration_ns) }}</dd>
      </div>
      <div>
        <dt class="font-mono text-[10px] uppercase tracking-[0.14em] text-muted">Assessed at</dt>
        <dd class="mt-1 font-mono text-sm text-highlighted">{{ audit.assessed_at }}</dd>
      </div>
    </dl>

    <section v-if="audit.error" class="border-b border-default py-6" aria-labelledby="audit-error-heading">
      <h2 id="audit-error-heading" class="text-lg font-semibold text-error">Redacted error</h2>
      <p class="mt-2 text-sm leading-6 text-muted">The analyzer reported an error. Details are redacted to avoid leaking sensitive context.</p>
      <p class="mt-3 break-words font-mono text-xs text-dimmed">{{ audit.error }}</p>
    </section>

    <section class="border-b border-default py-6" aria-labelledby="audit-findings-heading">
      <h2 id="audit-findings-heading" class="text-lg font-semibold text-highlighted">Findings</h2>
      <p v-if="audit.findings.length" class="mt-2 text-sm leading-6 text-muted">Every finding attributed to {{ audit.analyzer_id }}.</p>
      <ul v-if="audit.findings.length" class="mt-4 space-y-3" aria-label="Analyzer audit findings">
        <li v-for="(finding, index) in audit.findings" :key="`finding-${index}`" class="rounded-xl border border-warning/40 bg-warning/5 px-4 py-3 text-sm text-highlighted">
          <div class="flex flex-wrap items-center gap-x-3 gap-y-1">
            <span class="font-semibold">{{ finding.code }}</span>
            <span class="font-medium text-warning">Severity: {{ finding.severity }}</span>
            <span v-if="finding.policy" class="text-muted">Policy: {{ finding.policy }}</span>
            <span v-if="finding.analyzer_id" class="font-mono text-xs text-muted">{{ finding.analyzer_id }}</span>
          </div>
          <p v-if="finding.location?.path" class="mt-1 font-mono text-xs text-muted">{{ finding.location.path }}<template v-if="finding.location.start_byte !== undefined">:{{ finding.location.start_byte }}-{{ finding.location.end_byte ?? finding.location.start_byte }}</template><template v-else-if="finding.location.line !== undefined">:{{ finding.location.line }}<template v-if="finding.location.column !== undefined">:{{ finding.location.column }}</template></template></p>
          <p v-if="finding.message" class="mt-1">{{ finding.message }}</p>
          <span v-if="finding.consentable === false" class="mt-2 inline-block rounded-md border border-error/30 bg-error/5 px-2 py-0.5 text-xs font-medium text-error">Cannot be bypassed</span>
          <NuxtLink v-if="findingLocationRoute(finding)" :to="findingLocationRoute(finding)" class="mt-2 inline-block text-xs font-medium text-primary underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary">View source range</NuxtLink>
        </li>
      </ul>
      <p v-else class="mt-3 text-sm text-muted">No findings were reported by this analyzer.</p>
    </section>

    <section class="pt-6" aria-labelledby="audit-resource-heading">
      <h2 id="audit-resource-heading" class="text-lg font-semibold text-highlighted">Resource revision</h2>
      <p class="mt-2 text-sm leading-6 text-muted">Return to the authoritative resource revision for the full security context and installation status.</p>
      <div class="mt-4">
        <UButton :to="revisionRoute" color="neutral" variant="outline" size="sm">View resource revision</UButton>
      </div>
    </section>
  </article>
</template>
