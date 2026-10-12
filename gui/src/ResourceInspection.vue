<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import type { InspectionFile } from './api/client'
import type { WorkspaceController } from './controller'

interface TreeNode {
  id: string
  name: string
  path: string
  directory: boolean
  parentId: string | null
  file?: InspectionFile
  children: TreeNode[]
}

interface VisibleTreeNode extends TreeNode {
  depth: number
}

const props = defineProps<{ controller: WorkspaceController }>()
const open = defineModel<boolean>('open', { default: false })
const expandedDirectories = ref(new Set<string>())
const focusedNodeID = ref<string | null>(null)
const closeButton = ref<HTMLButtonElement | null>(null)
const treeItemElements = new Map<string, HTMLElement>()

const inspection = computed(() => props.controller.state.inspection)
const files = computed(() => inspection.value?.data?.files ?? [])

function sorted(nodes: TreeNode[]): TreeNode[] {
  nodes.sort((left, right) => Number(right.directory) - Number(left.directory) || left.name.localeCompare(right.name))
  for (const node of nodes) {
    sorted(node.children)
  }
  return nodes
}

const visibleTree = computed<VisibleTreeNode[]>(() => {
  const roots: TreeNode[] = []
  const directories = new Map<string, TreeNode>()
  for (const file of files.value) {
    const parts = file.path.split('/')
    let parentChildren = roots
    let parentID: string | null = null
    let directoryPath = ''
    for (const part of parts.slice(0, -1)) {
      directoryPath = directoryPath === '' ? part : `${directoryPath}/${part}`
      let directory = directories.get(directoryPath)
      if (directory == null) {
        directory = {
          id: `directory:${directoryPath}`,
          name: part,
          path: directoryPath,
          directory: true,
          parentId: parentID,
          children: [],
        }
        directories.set(directoryPath, directory)
        parentChildren.push(directory)
      }
      parentChildren = directory.children
      parentID = directory.id
    }
    parentChildren.push({
      id: file.id,
      name: parts.at(-1) ?? file.path,
      path: file.path,
      directory: false,
      parentId: parentID,
      file,
      children: [],
    })
  }

  const result: VisibleTreeNode[] = []
  const append = (nodes: TreeNode[], depth: number) => {
    for (const node of sorted(nodes)) {
      result.push({ ...node, depth })
      if (node.directory && expandedDirectories.value.has(node.path)) {
        append(node.children, depth + 1)
      }
    }
  }
  append(roots, 1)
  return result
})

const selectedFile = computed(() =>
  files.value.find((file) => file.id === inspection.value?.selectedFileId) ?? null,
)

function setTreeItemRef(id: string, element: Element | null) {
  if (element instanceof HTMLElement) {
    treeItemElements.set(id, element)
  } else {
    treeItemElements.delete(id)
  }
}

function focusTreeItem(id: string) {
  focusedNodeID.value = id
  void nextTick(() => treeItemElements.get(id)?.focus())
}

function toggleDirectory(node: VisibleTreeNode) {
  if (!node.directory) {
    return
  }
  const expanded = new Set(expandedDirectories.value)
  if (expanded.has(node.path)) {
    expanded.delete(node.path)
  } else {
    expanded.add(node.path)
  }
  expandedDirectories.value = expanded
}

function activateTreeItem(node: VisibleTreeNode) {
  focusedNodeID.value = node.id
  if (node.directory) {
    toggleDirectory(node)
  } else if (node.file != null) {
    props.controller.selectInspectionFile(node.file.id)
  }
}

function moveTreeFocus(node: VisibleTreeNode, direction: -1 | 1) {
  const index = visibleTree.value.findIndex((entry) => entry.id === node.id)
  const next = visibleTree.value[Math.min(visibleTree.value.length - 1, Math.max(0, index + direction))]
  if (next != null) {
    focusTreeItem(next.id)
  }
}

function expandTreeItem(node: VisibleTreeNode) {
  if (!node.directory) {
    return
  }
  if (!expandedDirectories.value.has(node.path)) {
    expandedDirectories.value = new Set(expandedDirectories.value).add(node.path)
    return
  }
  const child = visibleTree.value.find((entry) => entry.parentId === node.id)
  if (child != null) {
    focusTreeItem(child.id)
  }
}

function collapseTreeItem(node: VisibleTreeNode) {
  if (node.directory && expandedDirectories.value.has(node.path)) {
    const expanded = new Set(expandedDirectories.value)
    expanded.delete(node.path)
    expandedDirectories.value = expanded
    return
  }
  if (node.parentId != null) {
    focusTreeItem(node.parentId)
  }
}

watch(
  () => inspection.value?.rowId,
  (rowID, previousRowID) => {
    if (rowID !== previousRowID) {
      expandedDirectories.value = new Set()
      focusedNodeID.value = null
      treeItemElements.clear()
    }
  },
)

watch(
  () => inspection.value?.data?.primaryFileId,
  (primaryFileID) => {
    if (primaryFileID != null && focusedNodeID.value == null) {
      focusTreeItem(primaryFileID)
    }
  },
)

watch(
  () => inspection.value,
  (value) => {
    if (value == null) {
      open.value = false
    }
  },
)

watch(open, (isOpen) => {
  if (!isOpen) {
    props.controller.closeInspection()
  }
})

function retry() {
  const selectedFileID = inspection.value?.selectedFileId
  props.controller.retryInspection()
  if (selectedFileID != null && inspection.value?.data?.files.some((file) => file.id === selectedFileID)) {
    focusTreeItem(selectedFileID)
  } else {
    void nextTick(() => closeButton.value?.focus())
  }
}

function close() {
  open.value = false
}
</script>

<template>
  <UModal v-model:open="open" title="Resource inspection" aria-label="Resource inspection" :ui="{ content: 'resource-inspection-modal overflow-y-auto' }">
    <template #content>
      <section class="resource-inspection" aria-label="Resource inspection">
        <div class="resource-inspection__columns">
          <section class="resource-inspection__tree-panel" aria-label="Verified files">
            <ul role="tree" aria-label="Inspection files" class="resource-inspection__tree">
              <li v-for="node in visibleTree" :key="node.id" role="presentation">
                <button
                  :ref="(element) => setTreeItemRef(node.id, element as Element | null)"
                  type="button"
                  role="treeitem"
                  :aria-label="node.path"
                  :aria-level="node.depth"
                  :aria-expanded="node.directory ? expandedDirectories.has(node.path) : undefined"
                  :aria-selected="node.file != null && node.file.id === inspection?.selectedFileId"
                  :tabindex="focusedNodeID === node.id ? 0 : -1"
                  class="resource-inspection__treeitem"
                  :class="{ 'resource-inspection__treeitem--selected': node.file != null && node.file.id === inspection?.selectedFileId }"
                  :style="{ '--tree-depth': node.depth - 1 }"
                  @focus="focusedNodeID = node.id"
                  @click="activateTreeItem(node)"
                  @keydown.up.prevent="moveTreeFocus(node, -1)"
                  @keydown.down.prevent="moveTreeFocus(node, 1)"
                  @keydown.enter.prevent="activateTreeItem(node)"
                  @keydown.right.prevent="expandTreeItem(node)"
                  @keydown.left.prevent="collapseTreeItem(node)"
                >
                  <span v-if="node.directory" aria-hidden="true">{{ expandedDirectories.has(node.path) ? '▾' : '▸' }}</span>
                  <span>{{ node.name }}</span>
                </button>
              </li>
            </ul>
          </section>
          <section class="resource-inspection__preview-panel" aria-label="File preview">
            <h2 v-if="selectedFile" class="resource-inspection__path">{{ selectedFile.path }}</h2>
            <p v-if="inspection?.loading" role="status" aria-live="polite">
              {{ inspection.data == null ? 'Loading verified inspection…' : 'Loading selected file…' }}
            </p>
            <p v-else-if="inspection?.error" role="alert">{{ inspection.error }}</p>
            <p v-else-if="inspection?.data?.unsupportedMessage" role="status">{{ inspection.data.unsupportedMessage }}</p>
            <pre v-else-if="inspection?.data" class="resource-inspection__content" tabindex="0">{{ inspection.data.content }}</pre>
            <p v-else role="status">Select a verified file to preview it.</p>
          </section>
        </div>
        <div class="resource-inspection__actions">
          <button
            v-if="inspection?.error"
            type="button"
            aria-label="Retry inspection"
            @click="retry"
            @keydown.enter.prevent="retry"
          >Retry inspection</button>
          <button ref="closeButton" type="button" aria-label="Close inspection" @click="close">Close inspection</button>
        </div>
      </section>
    </template>
  </UModal>
</template>
