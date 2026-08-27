import { computed, ref, watch } from 'vue'
import { resourceInspectionFilePath } from '~/lib/api'
import { mergeFindings } from '~/lib/presentation'
import { highlightEscapedRawText, renderSafeMarkdown } from '~/lib/markdown'
import type { InspectionFile, InspectionManifest, ResourceKind, ResourceRevision } from '~/types/marketplace'

type ResourceInspectionPreviewOptions = {
  resource: Pick<ResourceRevision, 'content_digest' | 'findings'>
  source: string
  kind: ResourceKind
  name: string
  apiBase?: string
  inspection?: InspectionManifest
  primaryContent?: string
  loadInspectionFile?: (file: InspectionFile) => Promise<string | Blob>
}

export function useResourceInspectionPreview(options: ResourceInspectionPreviewOptions) {
  const inspection = computed(() => options.inspection)
  const primaryContent = computed(() => options.primaryContent)
  const resourceDigest = computed(() => options.resource.content_digest)
  // The revision record and the inspection manifest project the same analyzer
  // run and in practice repeat each other. Merge them into one list so a reader
  // sees each finding once; the manifest goes first because it carries the file
  // path and byte range that the revision record drops.
  const findings = computed(() => mergeFindings(inspection.value?.findings, options.resource.findings))
  const locatedFindings = computed(() => findings.value.filter(finding => finding.path !== ''))
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
  const selectedFindingDetails = computed(() => locatedFindings.value.filter(finding => finding.path === selectedFile.value?.path))
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
