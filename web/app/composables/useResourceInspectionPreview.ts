import { computed, ref, watch } from 'vue'
import { resourceInspectionFilePath } from '~/lib/api'
import { assessmentFindings } from '~/lib/presentation'
import { highlightEscapedRawText, renderSafeMarkdown } from '~/lib/markdown'
import type { InspectionFile, InspectionManifest, ResourceKind, ResourceRevision } from '~/types/marketplace'

type ResourceInspectionPreviewOptions = {
  resource: Pick<ResourceRevision, 'content_digest' | 'assessment'>
  source: string
  kind: ResourceKind
  name: string
  apiBase?: string
  inspection?: InspectionManifest
  primaryContent?: string
  loadInspectionFile?: (file: InspectionFile) => Promise<string | Blob>
}

function lineColumnByteRange(content: string, line: number | undefined, column: number | undefined): { startByte: number; endByte: number } | undefined {
  if (line === undefined || line < 1) return undefined
  const lines = content.split('\n')
  const lineText = lines[line - 1]
  if (lineText === undefined) return undefined
  const prefix = lines.slice(0, line - 1).join('\n') + (line > 1 ? '\n' : '')
  const characters = [...lineText]
  const characterIndex = Math.max(0, (column ?? 1) - 1)
  if (characterIndex > characters.length) return undefined
  const before = characters.slice(0, characterIndex).join('')
  const highlighted = characters[characterIndex] ?? ''
  const startByte = new TextEncoder().encode(prefix + before).length
  return { startByte, endByte: startByte + new TextEncoder().encode(highlighted).length }
}

export function useResourceInspectionPreview(options: ResourceInspectionPreviewOptions) {
  const inspection = computed(() => options.inspection)
  const primaryContent = computed(() => options.primaryContent)
  const resourceDigest = computed(() => options.resource.content_digest)
  const findings = computed(() => assessmentFindings(options.resource.assessment.findings))
  const primaryFile = computed(() => inspection.value?.files.find(file => file.path === inspection.value?.primary_path))
  const selectedDigest = ref('')
  const loadedContents = ref<Record<string, string>>({})
  const unavailableContents = ref<Record<string, boolean>>({})
  const loadingDigest = ref('')
  const selectedFile = computed(() => inspection.value?.files.find(file => file.content_digest === selectedDigest.value) ?? primaryFile.value)
  const selectedIsPrimary = computed(() => selectedFile.value?.path === inspection.value?.primary_path)
  const selectedIsMarkdown = computed(() => selectedFile.value?.media_type === 'text/markdown')
  const selectedIsImage = computed(() => selectedIsPrimary.value && selectedFile.value?.inspection_class === 'image')
  const selectedImageURL = computed(() => selectedFile.value && selectedIsImage.value
    ? resourceInspectionFilePath(options.apiBase, options.source, options.kind, options.name, resourceDigest.value, selectedFile.value.content_digest)
    : '')
  const selectedContent = computed(() => {
    if (!selectedFile.value || selectedFile.value.inspection_class !== 'text') return undefined
    if (selectedIsPrimary.value) return primaryContent.value
    return loadedContents.value[selectedFile.value.content_digest]
  })
  const selectedContentUnavailable = computed(() => Boolean(selectedFile.value && selectedFile.value.inspection_class === 'text' && (
    unavailableContents.value[selectedFile.value.content_digest] || (selectedIsPrimary.value && primaryContent.value === undefined)
  )))
  const displayFindings = computed(() => findings.value.map(finding => {
    if (finding.path !== selectedFile.value?.path || selectedContent.value === undefined || finding.startByte !== undefined) return finding
    const range = lineColumnByteRange(selectedContent.value, finding.line, finding.column)
    return range ? { ...finding, ...range } : finding
  }))
  const selectedFindingDetails = computed(() => displayFindings.value.filter(finding => finding.path === selectedFile.value?.path))
  const renderedContent = computed(() => selectedContent.value === undefined ? '' : renderSafeMarkdown(selectedContent.value))
  const highlightedContent = computed(() => selectedContent.value === undefined ? '' : highlightEscapedRawText(selectedContent.value, selectedFindingDetails.value.map(finding => ({
    index: finding.index,
    startByte: finding.startByte,
    endByte: finding.endByte,
    label: `${finding.code} (${finding.severity})`,
  }))))
  const textFiles = computed(() => inspection.value?.files.filter(file => file.inspection_class === 'text' && file.path !== inspection.value?.primary_path) ?? [])
  const selectableFiles = computed(() => {
    const primary = primaryFile.value
    return primary ? [primary, ...textFiles.value] : textFiles.value
  })
  const imageFiles = computed(() => inspection.value?.files.filter(file => file.inspection_class === 'image' && file.path !== inspection.value?.primary_path) ?? [])
  const binaryFiles = computed(() => inspection.value?.files.filter(file => file.inspection_class === 'metadata') ?? [])

  watch(() => inspection.value?.revision_digest, () => {
    selectedDigest.value = primaryFile.value?.content_digest ?? ''
    loadedContents.value = {}
    unavailableContents.value = {}
  }, { immediate: true })

  watch(selectedDigest, digest => {
    if (!digest || digest === primaryFile.value?.content_digest) return
    const file = inspection.value?.files.find(candidate => candidate.content_digest === digest)
    if (!file || file.inspection_class !== 'text' || loadedContents.value[digest] !== undefined || unavailableContents.value[digest]) return
    if (!options.loadInspectionFile) {
      unavailableContents.value = { ...unavailableContents.value, [digest]: true }
      return
    }
    loadingDigest.value = digest
    void options.loadInspectionFile(file).then(content => {
      if (typeof content !== 'string') throw new Error('inspection text response was not text')
      loadedContents.value = { ...loadedContents.value, [digest]: content }
    }).catch(() => {
      unavailableContents.value = { ...unavailableContents.value, [digest]: true }
    }).finally(() => {
      if (loadingDigest.value === digest) loadingDigest.value = ''
    })
  })

  function inspectionFileURL(digest: string): string {
    return resourceInspectionFilePath(options.apiBase, options.source, options.kind, options.name, resourceDigest.value, digest)
  }

  return {
    binaryFiles,
    displayFindings,
    findings,
    highlightedContent,
    imageFiles,
    inspectionFileURL,
    loadingDigest,
    renderedContent,
    selectableFiles,
    selectedContent,
    selectedContentUnavailable,
    selectedDigest,
    selectedFile,
    selectedFindingDetails,
    selectedImageURL,
    selectedIsImage,
    selectedIsMarkdown,
    selectedIsPrimary,
    textFiles,
  }
}
