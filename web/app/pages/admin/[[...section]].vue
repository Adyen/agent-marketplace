<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import {
  AdminError,
  adminLoginHref,
  cloneAdmin,
  createAdminApi,
  lifecycleActions,
  newAdminRegistry,
  restoreRegistrySettings,
} from "~/lib/admin";
import type {
  AdminChange,
  AdminConfiguration,
  AdminInstance,
  AdminProfiles,
  AdminProposal,
  AdminRegistry,
  AdminSession,
} from "~/lib/admin";
import {
  applicableLifecycleActions,
  destructiveLifecycleActions,
  lifecycleBadge,
  operationLabel,
} from "~/lib/admin-presentation";
import type { Crumb } from "~/components/AdminBreadcrumb.vue";

const route = useRoute();
const api = createAdminApi();
const session = ref<AdminSession>();
const configuration = ref<AdminConfiguration>();
const profiles = ref<AdminProfiles>({ profiles: [], source_harnesses: [] });
const instance = ref<AdminInstance>();
const changes = ref<AdminChange[]>([]);
const hasOlder = ref(false);
const pending = ref(true);
const error = ref("");
const errorCode = ref("");
const authenticationMode = ref("");
const draft = ref<AdminProposal>();
const editorKey = ref(0);
const search = ref("");
const lifecycleFilter = ref("current");
const parts = computed(() =>
  Array.isArray(route.params.section)
    ? route.params.section
    : route.params.section
      ? [route.params.section]
      : [],
);
const section = computed(() => parts.value[0] || "registries");
const registryID = computed(() => parts.value[1]);
const tab = computed(() => parts.value[2] || "configuration");
const registry = computed(() =>
  configuration.value?.snapshot.registries.find(
    (value) => value.id === registryID.value,
  ),
);
const registries = computed(() =>
  (configuration.value?.snapshot.registries || []).filter(
    (value) =>
      `${value.slug} ${value.id} ${value.origin}`
        .toLowerCase()
        .includes(search.value.toLowerCase()) &&
      (lifecycleFilter.value === "all" ||
        (lifecycleFilter.value === "current"
          ? !value.retired
          : value.lifecycle === lifecycleFilter.value)),
  ),
);
const registryChanges = computed(() =>
  changes.value.filter((value) =>
    value.snapshot.registries.some((item) => item.id === registryID.value),
  ),
);
const sectionTitles: Record<string, string> = {
  policies: "Policies",
  activity: "Activity",
  instance: "Instance",
  registries: "Registries",
};
const sectionIcons: Record<string, string> = {
  registries: "i-lucide-library",
  policies: "i-lucide-scale",
  activity: "i-lucide-history",
  instance: "i-lucide-server",
};
const registryTabs = [
  { id: "configuration", label: "Configuration" },
  { id: "indexing", label: "Indexing history" },
  { id: "resources", label: "Resources" },
  { id: "findings", label: "Findings" },
];
const title = computed(
  () =>
    registry.value?.slug || sectionTitles[section.value] || "Administration",
);
const crumbs = computed<Crumb[]>(() => {
  const items: Crumb[] = [
    { label: "Administration", to: "/admin" },
    {
      label: sectionTitles[section.value] || section.value,
      to: `/admin/${section.value}`,
    },
  ];
  if (registryID.value) {
    items.push({
      label: registry.value?.slug || registryID.value,
      to: registryPath(registryID.value),
    });
    const current = registryTabs.find((value) => value.id === tab.value);
    if (current && tab.value !== "configuration")
      items.push({ label: current.label });
  }
  return items;
});
const lifecycleFilterItems = computed(() => [
  { label: "Current registries", value: "current" },
  { label: "All, including retired", value: "all" },
  ...[
    ...new Set(
      (configuration.value?.snapshot.registries || []).map(
        (value) => value.lifecycle,
      ),
    ),
  ].map((value) => ({ label: `${lifecycleBadge(value).label} only`, value })),
]);
const registryActions = computed(() =>
  registry.value
    ? applicableLifecycleActions(registry.value).map(
        (action) => lifecycleActions.find((value) => value.action === action)!,
      )
    : [],
);
useHead(() => ({
  title: `${title.value} · Administration · Agent Marketplace`,
  meta: [{ name: "robots", content: "noindex, nofollow, noarchive" }],
}));

function describeError(value: unknown) {
  errorCode.value = value instanceof AdminError ? value.code : "";
  if (value instanceof AdminError) {
    const mode = value.fields.authentication_mode;
    if (mode === "oidc" || mode === "trusted_proxy")
      authenticationMode.value = mode;
    if (value.code === "unauthenticated")
      return authenticationMode.value === "oidc"
        ? "Your administration session is missing or expired. Sign in again; nothing is replayed automatically."
        : "Authenticate through your deployment identity proxy, then try again; nothing is replayed automatically.";
    if (value.code === "forbidden")
      return "Your identity is not authorized to administer this Marketplace.";
    return `${value.code}: ${value.message}`;
  }
  return "Administration could not be loaded.";
}
const appBase = useRuntimeConfig().app.baseURL.replace(/\/+$/, "");
const loginHref = computed(() =>
  adminLoginHref(`${appBase}${route.fullPath}`, `${appBase}/`),
);
async function logout() {
  try {
    await api.logout();
  } catch {
    error.value =
      "Sign-out could not be confirmed. Your administration session may still be active.";
    errorCode.value = "logout_failed";
    return;
  }
  session.value = undefined;
  configuration.value = undefined;
  draft.value = undefined;
  error.value = "You have been signed out of Marketplace administration.";
  errorCode.value = "unauthenticated";
}
async function load() {
  pending.value = true;
  error.value = "";
  errorCode.value = "";
  authenticationMode.value = "";
  try {
    session.value = await api.session();
    authenticationMode.value = session.value.authentication_mode;
    configuration.value = await api.configuration();
    profiles.value = await api.profiles();
    if (section.value === "instance") instance.value = await api.instance();
    if (section.value === "activity" || registryID.value) await loadActivity();
  } catch (value) {
    error.value = describeError(value);
  } finally {
    pending.value = false;
  }
}
async function loadActivity(older = false) {
  try {
    const before = older ? changes.value.at(-1)?.revision : undefined;
    const result = await api.activity(before);
    changes.value = older
      ? [...changes.value, ...result.changes]
      : result.changes;
    hasOlder.value = result.changes.length === 25;
  } catch (value) {
    error.value = describeError(value);
  }
}
function edit(proposal: AdminProposal) {
  draft.value = cloneAdmin(proposal);
  editorKey.value++;
}
function editRegistry(value: AdminRegistry) {
  edit({ action: "upsert_registry", registry: value, reason: "" });
}
function restore(change: AdminChange, id?: string) {
  if (!configuration.value) return;
  if (!id) {
    edit({
      action: "set_policy",
      license_policy: change.snapshot.license_policy,
      distribution_policy: change.snapshot.distribution_policy,
      reason: `Restore policy settings from revision ${change.revision}`,
    });
    return;
  }
  const current = configuration.value.snapshot.registries.find(
    (value) => value.id === id,
  );
  const previous = change.snapshot.registries.find((value) => value.id === id);
  if (current && previous)
    edit({
      action: "upsert_registry",
      registry: restoreRegistrySettings(current, previous),
      reason: `Restore registry settings from revision ${change.revision}`,
    });
}
function accepted(change: AdminChange) {
  configuration.value = {
    revision: change.revision,
    snapshot: change.snapshot,
  };
}
function registryPath(id: string, selectedTab = "configuration") {
  return `/admin/registries/${encodeURIComponent(id)}/${selectedTab}`;
}
function restorableRegistries(change: AdminChange) {
  return change.snapshot.registries.filter((old) =>
    configuration.value?.snapshot.registries.some(
      (current) => current.id === old.id,
    ),
  );
}
onMounted(load);
watch(
  () => route.fullPath,
  () => {
    draft.value = undefined;
    changes.value = [];
    void load();
  },
);
</script>

<template>
  <main
    id="main-content"
    class="mx-auto min-h-screen max-w-7xl space-y-6 px-4 py-6 text-default sm:px-6 sm:py-8"
    tabindex="-1"
  >
    <header class="space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <AdminBreadcrumb :items="crumbs" />
        <div v-if="session" class="flex items-center gap-2">
          <p class="flex items-center gap-1.5 text-xs text-muted">
            <UIcon
              name="i-lucide-user-round"
              class="size-3.5"
              aria-hidden="true"
            /><span class="font-medium text-toned">{{
              session.display || session.actor.subject
            }}</span>
            <template v-if="configuration"
              ><span aria-hidden="true">·</span
              ><span>Revision {{ configuration.revision }}</span
              ><AdminHint
                text="Every accepted change creates a new configuration revision. Drafts you prepare here stay in this browser tab until you apply them."
            /></template>
          </p>
          <UButton
            v-if="session.authentication_mode === 'oidc'"
            size="xs"
            color="neutral"
            variant="outline"
            icon="i-lucide-log-out"
            @click="logout"
            >Sign out</UButton
          >
        </div>
      </div>
      <div
        class="flex flex-wrap items-end justify-between gap-4 border-b border-default pb-4"
      >
        <div class="flex min-w-0 flex-wrap items-center gap-3">
          <h1
            class="truncate text-2xl font-semibold tracking-tight text-highlighted sm:text-3xl"
          >
            {{ title }}
          </h1>
          <AdminStatusBadge
            v-if="registry"
            :value="registry.lifecycle"
            kind="lifecycle"
          />
        </div>
        <nav
          aria-label="Administration"
          class="flex flex-wrap gap-1 rounded-lg bg-[var(--am-color-bg-subtle)] p-1"
        >
          <UButton
            v-for="item in ['registries', 'policies', 'activity', 'instance']"
            :key="item"
            :to="`/admin/${item}`"
            :icon="sectionIcons[item]"
            size="sm"
            :variant="section === item ? 'solid' : 'ghost'"
            :color="section === item ? 'primary' : 'neutral'"
            :aria-current="section === item ? 'page' : undefined"
            >{{ sectionTitles[item] }}</UButton
          >
        </nav>
      </div>
    </header>

    <div
      v-if="pending"
      role="status"
      aria-busy="true"
      class="flex items-center gap-2 text-sm text-muted"
    >
      <UIcon
        name="i-lucide-loader-circle"
        class="size-4 animate-spin"
        aria-hidden="true"
      />Loading administration…
    </div>
    <div
      v-else-if="error"
      role="alert"
      class="space-y-3 rounded-xl border border-error bg-[var(--am-color-bg-surface)] p-5"
    >
      <p class="font-medium text-error">
        {{
          errorCode === "unauthenticated"
            ? "Sign in required"
            : errorCode === "forbidden"
              ? "Not authorized"
              : errorCode === "logout_failed"
                ? "Sign-out not confirmed"
                : "Administration is unavailable"
        }}
      </p>
      <p class="text-sm text-toned">{{ error }}</p>
      <div class="flex flex-wrap gap-2">
        <UButton
          v-if="
            errorCode === 'unauthenticated' && authenticationMode === 'oidc'
          "
          :to="loginHref"
          icon="i-lucide-log-in"
          external
          >Sign in with Okta</UButton
        ><UButton
          color="neutral"
          variant="outline"
          icon="i-lucide-refresh-cw"
          @click="load"
          >Try again</UButton
        >
      </div>
    </div>

    <template v-else-if="session && configuration">
      <AdminChangeEditor
        v-if="draft"
        :key="editorKey"
        :initial="draft"
        :configuration="configuration"
        :profiles="profiles"
        @accepted="accepted"
        @cancel="draft = undefined"
      />

      <template v-else-if="section === 'registries' && !registryID">
        <div class="flex flex-wrap items-end gap-3">
          <AdminField
            id="registry-search"
            label="Search"
            class="min-w-56 grow sm:max-w-sm"
            ><UInput
              id="registry-search"
              v-model="search"
              type="search"
              icon="i-lucide-search"
              placeholder="Slug, ID or origin"
              class="w-full"
          /></AdminField>
          <AdminField id="lifecycle-filter" label="Show" class="min-w-48"
            ><USelect
              id="lifecycle-filter"
              v-model="lifecycleFilter"
              :items="lifecycleFilterItems"
              class="w-full"
          /></AdminField>
          <UButton
            icon="i-lucide-plus"
            class="ml-auto"
            @click="editRegistry(newAdminRegistry())"
            >Add registry</UButton
          >
        </div>
        <div
          class="overflow-hidden rounded-xl border border-default bg-[var(--am-color-bg-surface)]"
        >
          <table class="w-full text-left text-sm">
            <caption class="sr-only">
              Managed registries
            </caption>
            <thead
              class="bg-[var(--am-color-bg-subtle)] text-xs uppercase tracking-wide text-muted"
            >
              <tr>
                <th class="px-4 py-2.5 font-semibold">Registry</th>
                <th class="hidden px-4 py-2.5 font-semibold md:table-cell">
                  Origin
                </th>
                <th class="hidden px-4 py-2.5 font-semibold sm:table-cell">
                  Ref
                </th>
                <th class="px-4 py-2.5 font-semibold">Lifecycle</th>
                <th class="px-2 py-2.5"><span class="sr-only">Open</span></th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="item in registries"
                :key="item.id"
                class="relative border-t border-default transition-colors hover:bg-[var(--am-color-bg-hover)]"
              >
                <th scope="row" class="px-4 py-3 font-normal">
                  <NuxtLink
                    :to="registryPath(item.id)"
                    class="font-semibold text-highlighted after:absolute after:inset-0 hover:underline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
                    >{{ item.slug }}</NuxtLink
                  ><UBadge
                    v-if="
                      item.rejected_resources &&
                      item.rejected_resources !==
                        configuration.snapshot.distribution_policy
                          .rejected_resources
                    "
                    color="warning"
                    variant="subtle"
                    size="sm"
                    class="ml-2"
                    >Exposure override</UBadge
                  >
                  <p class="mt-0.5 font-mono text-xs text-muted">
                    {{ item.id }}
                  </p>
                </th>
                <td
                  class="hidden max-w-xs truncate px-4 py-3 font-mono text-xs text-toned md:table-cell"
                  :title="item.origin"
                >
                  {{ item.origin }}
                </td>
                <td
                  class="hidden px-4 py-3 font-mono text-xs text-toned sm:table-cell"
                >
                  {{ item.ref || "Default branch" }}
                </td>
                <td class="px-4 py-3">
                  <AdminStatusBadge :value="item.lifecycle" kind="lifecycle" />
                </td>
                <td class="px-2 py-3 text-right">
                  <UIcon
                    name="i-lucide-chevron-right"
                    class="size-4 text-dimmed"
                    aria-hidden="true"
                  />
                </td>
              </tr>
            </tbody>
          </table>
          <div
            v-if="!registries.length"
            class="space-y-2 p-8 text-center text-sm text-muted"
          >
            <p>
              {{
                configuration.snapshot.registries.length
                  ? "No registries match this search or filter."
                  : "No registries are managed yet."
              }}
            </p>
            <UButton
              v-if="!configuration.snapshot.registries.length"
              icon="i-lucide-plus"
              variant="outline"
              color="neutral"
              @click="editRegistry(newAdminRegistry())"
              >Add your first registry</UButton
            >
          </div>
        </div>
        <p class="flex items-center gap-1.5 text-xs text-muted">
          {{ registries.length }} of
          {{ configuration.snapshot.registries.length }} registries shown.
          Lifecycle is the desired state, not indexing health.<AdminHint
            text="Indexed commit, last successful run and publication status are not exposed by the admin API yet. Open a registry's Indexing history for recorded attempts."
          />
        </p>
      </template>

      <template v-else-if="section === 'registries' && registry">
        <div
          class="rounded-xl border border-default bg-[var(--am-color-bg-surface)] p-5"
        >
          <dl
            class="grid gap-x-6 gap-y-4 text-sm sm:grid-cols-2 lg:grid-cols-4"
          >
            <div class="sm:col-span-2">
              <dt
                class="text-xs font-semibold uppercase tracking-wide text-muted"
              >
                Origin
              </dt>
              <dd class="mt-1 break-all font-mono text-highlighted">
                {{ registry.origin }}
              </dd>
            </div>
            <div>
              <dt
                class="text-xs font-semibold uppercase tracking-wide text-muted"
              >
                Ref
              </dt>
              <dd class="mt-1 font-mono text-highlighted">
                {{ registry.ref || "Default branch" }}
              </dd>
            </div>
            <div>
              <dt
                class="flex items-center gap-1 text-xs font-semibold uppercase tracking-wide text-muted"
              >
                Generation<AdminHint
                  text="Increments each time this registry's settings change. Indexed commit and publication revision are not reported by the admin API yet."
                />
              </dt>
              <dd class="mt-1 text-highlighted">
                {{ registry.generation }}
                <span class="text-muted"
                  >at revision {{ configuration.revision }}</span
                >
              </dd>
            </div>
            <div class="sm:col-span-2 lg:col-span-4">
              <dt class="sr-only">Identity</dt>
              <dd><AdminIdChip label="ID" :value="registry.id" /></dd>
            </div>
          </dl>
        </div>
        <nav
          aria-label="Registry sections"
          class="flex flex-wrap gap-1 border-b border-default"
        >
          <NuxtLink
            v-for="item in registryTabs"
            :key="item.id"
            :to="registryPath(registry.id, item.id)"
            :aria-current="tab === item.id ? 'page' : undefined"
            class="-mb-px border-b-2 px-3 py-2 text-sm font-medium transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary"
            :class="
              tab === item.id
                ? 'border-primary text-highlighted'
                : 'border-transparent text-muted hover:border-default hover:text-highlighted'
            "
            >{{ item.label }}</NuxtLink
          >
        </nav>

        <section
          v-if="tab === 'configuration'"
          class="grid gap-5 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]"
        >
          <div class="space-y-5">
            <div
              class="rounded-xl border border-default bg-[var(--am-color-bg-surface)] p-5"
            >
              <div class="flex flex-wrap items-center justify-between gap-3">
                <h2 class="text-lg font-semibold text-highlighted">Settings</h2>
                <UButton
                  icon="i-lucide-pencil"
                  size="sm"
                  @click="editRegistry(registry)"
                  >Edit configuration</UButton
                >
              </div>
              <dl class="mt-4 grid gap-x-6 gap-y-4 text-sm sm:grid-cols-2">
                <div class="sm:col-span-2">
                  <dt
                    class="text-xs font-semibold uppercase tracking-wide text-muted"
                  >
                    Source roots
                  </dt>
                  <dd class="mt-1">
                    <ol
                      class="divide-y divide-default rounded-lg border border-default"
                    >
                      <li
                        v-for="(root, index) in registry.roots"
                        :key="index"
                        class="flex items-center gap-3 px-3 py-2"
                      >
                        <span class="w-5 text-xs text-muted">{{
                          index + 1
                        }}</span
                        ><code class="font-mono text-highlighted">{{
                          root.path
                        }}</code
                        ><span class="ml-auto text-xs text-muted">{{
                          root.source_harness || "Autodetect"
                        }}</span>
                      </li>
                    </ol>
                  </dd>
                </div>
                <div>
                  <dt
                    class="text-xs font-semibold uppercase tracking-wide text-muted"
                  >
                    Acquisition
                  </dt>
                  <dd class="mt-1 text-highlighted">
                    {{
                      registry.acquisition === "mirror"
                        ? "Mirror"
                        : "Index only"
                    }}
                  </dd>
                </div>
                <div>
                  <dt
                    class="text-xs font-semibold uppercase tracking-wide text-muted"
                  >
                    Trust profile
                  </dt>
                  <dd class="mt-1 text-highlighted">
                    {{ registry.profile_id || "None" }}
                  </dd>
                </div>
                <div>
                  <dt
                    class="text-xs font-semibold uppercase tracking-wide text-muted"
                  >
                    Publisher
                  </dt>
                  <dd class="mt-1 text-highlighted">
                    {{ registry.publisher.name || "Not set" }}
                  </dd>
                  <dd
                    v-if="registry.publisher.url"
                    class="break-all font-mono text-xs text-muted"
                  >
                    {{ registry.publisher.url }}
                  </dd>
                </div>
                <div>
                  <dt
                    class="text-xs font-semibold uppercase tracking-wide text-muted"
                  >
                    Default license
                  </dt>
                  <dd class="mt-1 text-highlighted">
                    {{ registry.license || "Not set" }}
                  </dd>
                </div>
                <div class="sm:col-span-2">
                  <dt
                    class="text-xs font-semibold uppercase tracking-wide text-muted"
                  >
                    License policy
                  </dt>
                  <dd class="mt-1 text-highlighted">
                    {{
                      registry.license_override
                        ? `Override: ${registry.license_override.allowlist?.join(", ") || "inherited allowlist"}; undeclared ${registry.license_override.undeclared || "inherited"}`
                        : "Inherits the global policy"
                    }}
                  </dd>
                </div>
                <div class="sm:col-span-2">
                  <dt
                    class="text-xs font-semibold uppercase tracking-wide text-muted"
                  >
                    Rejected resources
                  </dt>
                  <dd class="mt-1 text-highlighted">
                    {{
                      registry.rejected_resources
                        ? `Override: ${registry.rejected_resources}`
                        : `Inherits global ${configuration.snapshot.distribution_policy.rejected_resources}`
                    }}
                  </dd>
                </div>
              </dl>
            </div>
            <AdminHint summary="View the managed representation">
              <pre
                class="overflow-auto rounded bg-[var(--am-color-bg-subtle)] p-3 font-mono text-xs"
                >{{ JSON.stringify(registry, null, 2) }}</pre
              >
              <p class="text-xs">
                Read-only JSON; also valid YAML 1.2.
              </p></AdminHint
            >
          </div>
          <aside
            class="space-y-3 rounded-xl border border-default bg-[var(--am-color-bg-surface)] p-5"
            aria-labelledby="registry-actions-heading"
          >
            <h2
              id="registry-actions-heading"
              class="text-lg font-semibold text-highlighted"
            >
              Actions
            </h2>
            <p class="text-xs text-muted">
              Each action opens a draft to review before anything is applied.
            </p>
            <div
              v-for="action in registryActions"
              :key="action.action"
              class="flex items-start gap-3 border-t border-default pt-3"
            >
              <UButton
                :color="
                  destructiveLifecycleActions.has(action.action)
                    ? 'error'
                    : 'neutral'
                "
                variant="outline"
                size="sm"
                class="shrink-0"
                @click="
                  edit({
                    action: action.action,
                    registry_id: registry.id,
                    reason: '',
                  })
                "
                >{{ action.label }}</UButton
              >
              <p class="text-xs leading-5 text-muted">
                {{ action.description }}
              </p>
            </div>
          </aside>
        </section>

        <section
          v-else-if="tab === 'resources'"
          class="space-y-3 rounded-xl border border-default bg-[var(--am-color-bg-surface)] p-5"
        >
          <h2 class="text-lg font-semibold text-highlighted">
            Published resources
          </h2>
          <p class="text-sm text-toned">
            The public catalog lists what is currently discoverable from this
            registry.
          </p>
          <UButton
            :to="{ path: '/', query: { registry: registry.slug } }"
            color="neutral"
            variant="outline"
            icon="i-lucide-external-link"
            >Browse published resources</UButton
          ><AdminHint
            summary="Why acquired or blocked content isn't listed here"
            ><p>
              The admin API does not expose acquired-but-unpublished or blocked
              resources yet, so this view can only link to the public catalog.
            </p></AdminHint
          >
        </section>

        <section
          v-else-if="tab === 'findings'"
          class="space-y-3 rounded-xl border border-default bg-[var(--am-color-bg-surface)] p-5"
        >
          <h2 class="text-lg font-semibold text-highlighted">Findings</h2>
          <p class="text-sm text-toned">
            Structured discovery, assessment, license, withdrawal and artifact
            findings are not available from the admin API yet. This is not
            evidence that the registry has no findings.
          </p>
          <dl class="grid gap-4 text-sm sm:grid-cols-2">
            <div>
              <dt
                class="text-xs font-semibold uppercase tracking-wide text-muted"
              >
                License settings
              </dt>
              <dd class="mt-1 text-highlighted">
                {{
                  registry.license_override
                    ? "Registry override"
                    : "Inherited global policy"
                }}
              </dd>
            </div>
            <div>
              <dt
                class="text-xs font-semibold uppercase tracking-wide text-muted"
              >
                Lifecycle
              </dt>
              <dd class="mt-1">
                <AdminStatusBadge
                  :value="registry.lifecycle"
                  kind="lifecycle"
                />
              </dd>
            </div>
          </dl>
          <UButton
            to="/admin/policies"
            color="neutral"
            variant="outline"
            size="sm"
            >Inspect policy configuration</UButton
          >
        </section>

        <section v-else-if="tab === 'indexing'" class="space-y-6">
          <AdminAttempts :key="registry.id" :registry-id="registry.id" />
          <div class="space-y-3">
            <div class="flex items-center gap-1.5">
              <h3 class="text-base font-semibold text-highlighted">
                Configuration requests
              </h3>
              <AdminHint
                text="Instance-wide configuration changes made while this registry existed. Their linked jobs may concern other registries, and their completion is separate from the registry indexing outcomes above."
              />
            </div>
            <article
              v-for="change in registryChanges"
              :key="`${change.revision}-${change.request_id}`"
              class="space-y-3 rounded-xl border border-default bg-[var(--am-color-bg-surface)] p-4"
            >
              <div class="flex flex-wrap items-center justify-between gap-2">
                <h4 class="font-medium text-highlighted">
                  {{ operationLabel(change.operation) }}
                  <span class="text-muted"
                    >· revision {{ change.revision }}</span
                  >
                </h4>
                <AdminTime
                  :value="change.created_at"
                  class="text-xs text-muted"
                />
              </div>
              <p class="text-sm text-toned">{{ change.reason }}</p>
              <AdminJobs :ids="change.job_ids || []" />
              <UButton
                color="neutral"
                variant="outline"
                size="sm"
                icon="i-lucide-undo-2"
                @click="restore(change, registry.id)"
                >Restore settings from revision {{ change.revision }}</UButton
              >
            </article>
            <UButton
              v-if="hasOlder"
              color="neutral"
              variant="outline"
              size="sm"
              @click="loadActivity(true)"
              >Load older changes</UButton
            >
            <p v-if="!registryChanges.length" class="text-sm text-muted">
              No linked configuration changes in this page of history.
            </p>
          </div>
        </section>
        <p v-else>Unknown registry section.</p>
      </template>

      <section
        v-else-if="section === 'policies'"
        class="grid gap-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]"
      >
        <div
          class="space-y-4 rounded-xl border border-default bg-[var(--am-color-bg-surface)] p-5"
        >
          <div class="flex flex-wrap items-center justify-between gap-3">
            <h2 class="text-lg font-semibold text-highlighted">
              Global policy
            </h2>
            <UButton
              icon="i-lucide-pencil"
              size="sm"
              @click="
                edit({
                  action: 'set_policy',
                  license_policy: configuration.snapshot.license_policy,
                  distribution_policy:
                    configuration.snapshot.distribution_policy,
                  reason: '',
                })
              "
              >Edit policies</UButton
            >
          </div>
          <dl class="grid gap-4 text-sm">
            <div>
              <dt
                class="text-xs font-semibold uppercase tracking-wide text-muted"
              >
                Allowlist
              </dt>
              <dd class="mt-1.5 flex flex-wrap gap-1.5">
                <template
                  v-if="configuration.snapshot.license_policy.allowlist?.length"
                  ><UBadge
                    v-for="license in configuration.snapshot.license_policy
                      .allowlist"
                    :key="license"
                    color="neutral"
                    variant="outline"
                    size="sm"
                    class="font-mono"
                    >{{ license }}</UBadge
                  ></template
                ><span v-else class="flex items-center gap-1 text-warning"
                  >Empty — enforcement disabled<AdminHint
                    text="With no allowlisted licenses, resources are published regardless of their declared license."
                /></span>
              </dd>
            </div>
            <div>
              <dt
                class="text-xs font-semibold uppercase tracking-wide text-muted"
              >
                Undeclared licenses
              </dt>
              <dd class="mt-1 capitalize text-highlighted">
                {{ configuration.snapshot.license_policy.undeclared }}
              </dd>
            </div>
            <div>
              <dt
                class="text-xs font-semibold uppercase tracking-wide text-muted"
              >
                Rejected resources
              </dt>
              <dd class="mt-1 text-highlighted">
                {{
                  configuration.snapshot.distribution_policy
                    .rejected_resources === "expose"
                    ? "Expose as operator-approved"
                    : "Block publication"
                }}
              </dd>
            </div>
          </dl>
          <AdminHint summary="What this policy does not tell you"
            ><p>
              License impact analysis is unavailable: no affected-resource or
              retained-revision counts are claimed. Hidden-content analysis and
              resource or time limits remain deployment-managed.
            </p></AdminHint
          >
        </div>
        <div
          class="rounded-xl border border-default bg-[var(--am-color-bg-surface)]"
        >
          <h2
            class="border-b border-default px-5 py-4 text-lg font-semibold text-highlighted"
          >
            Per-registry policy
          </h2>
          <ul class="divide-y divide-default">
            <li
              v-for="item in configuration.snapshot.registries"
              :key="item.id"
              class="flex flex-wrap items-center justify-between gap-2 px-5 py-3 text-sm"
            >
              <NuxtLink
                :to="registryPath(item.id)"
                class="font-medium text-highlighted hover:underline"
                >{{ item.slug }}</NuxtLink
              >
              <span class="flex flex-wrap items-center justify-end gap-2"
                ><UBadge
                  v-if="item.license_override"
                  color="warning"
                  variant="subtle"
                  size="sm"
                  >License override</UBadge
                ><UBadge
                  v-if="item.rejected_resources"
                  color="warning"
                  variant="subtle"
                  size="sm"
                  >Rejected: {{ item.rejected_resources }}</UBadge
                ><UBadge
                  v-if="!item.license_override && !item.rejected_resources"
                  color="neutral"
                  variant="subtle"
                  size="sm"
                  >Inherits global</UBadge
                ></span
              >
            </li>
          </ul>
        </div>
      </section>

      <section v-else-if="section === 'activity'" class="space-y-4">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <p class="text-sm text-toned">
            Every configuration and operational request, newest first.
          </p>
          <UButton
            color="neutral"
            variant="outline"
            size="sm"
            icon="i-lucide-refresh-cw"
            @click="loadActivity()"
            >Refresh</UButton
          >
        </div>
        <article
          v-for="change in changes"
          :key="`${change.revision}-${change.request_id}`"
          class="space-y-3 rounded-xl border border-default bg-[var(--am-color-bg-surface)] p-5"
        >
          <div class="flex flex-wrap items-start justify-between gap-2">
            <div>
              <h3 class="font-semibold text-highlighted">
                {{ operationLabel(change.operation) }}
              </h3>
              <p class="mt-0.5 text-xs text-muted">
                Revision {{ change.parent_revision }} → {{ change.revision }} ·
                by
                <span class="font-medium text-toned">{{
                  change.actor.subject
                }}</span>
                · <AdminTime :value="change.created_at" />
              </p>
            </div>
            <AdminIdChip label="Request" :value="change.request_id" />
          </div>
          <blockquote class="border-l-2 border-default pl-3 text-sm text-toned">
            {{ change.reason }}
          </blockquote>
          <AdminJobs :ids="change.job_ids || []" />
          <AdminHint summary="Settings at this revision">
            <pre
              class="overflow-auto rounded bg-[var(--am-color-bg-subtle)] p-3 font-mono text-xs"
              >{{ JSON.stringify(change.snapshot, null, 2) }}</pre
            >
          </AdminHint>
          <div
            class="flex flex-wrap items-center gap-2 border-t border-default pt-3"
          >
            <span class="flex items-center gap-1 text-xs text-muted"
              >Restore<AdminHint
                text="Opens a new draft against the current settings for you to review. Lifecycle, aliases, historical origins and non-license overrides are kept as they are now."
            /></span>
            <UButton
              color="neutral"
              variant="outline"
              size="xs"
              @click="restore(change)"
              >Policy settings</UButton
            >
            <UButton
              v-for="item in restorableRegistries(change)"
              :key="item.id"
              color="neutral"
              variant="outline"
              size="xs"
              @click="restore(change, item.id)"
              >{{ item.slug }} settings</UButton
            >
          </div>
        </article>
        <p
          v-if="!changes.length"
          class="rounded-xl border border-dashed border-default p-8 text-center text-sm text-muted"
        >
          No changes have been recorded yet.
        </p>
        <UButton
          v-if="hasOlder"
          color="neutral"
          variant="outline"
          size="sm"
          @click="loadActivity(true)"
          >Load older changes</UButton
        >
      </section>

      <section
        v-else-if="section === 'instance' && instance"
        class="space-y-4 rounded-xl border border-default bg-[var(--am-color-bg-surface)] p-5"
      >
        <div class="flex items-center gap-2">
          <h2 class="text-lg font-semibold text-highlighted">
            Deployment-owned instance
          </h2>
          <UBadge color="neutral" variant="subtle" size="sm">Read only</UBadge>
        </div>
        <dl class="grid gap-x-6 gap-y-4 text-sm sm:grid-cols-2">
          <div>
            <dt
              class="text-xs font-semibold uppercase tracking-wide text-muted"
            >
              Name
            </dt>
            <dd class="mt-1 text-highlighted">{{ instance.name }}</dd>
          </div>
          <div>
            <dt
              class="text-xs font-semibold uppercase tracking-wide text-muted"
            >
              Version
            </dt>
            <dd class="mt-1 font-mono text-highlighted">
              {{ instance.version }}
            </dd>
          </div>
          <div>
            <dt
              class="text-xs font-semibold uppercase tracking-wide text-muted"
            >
              Management mode
            </dt>
            <dd class="mt-1 capitalize text-highlighted">
              {{ instance.management_mode }}
            </dd>
          </div>
          <div>
            <dt
              class="text-xs font-semibold uppercase tracking-wide text-muted"
            >
              Configuration revision
            </dt>
            <dd class="mt-1 text-highlighted">{{ instance.revision }}</dd>
          </div>
          <div class="sm:col-span-2">
            <dt
              class="flex items-center gap-1 text-xs font-semibold uppercase tracking-wide text-muted"
            >
              Worker observation<AdminHint
                text="A recorded observation, not a heartbeat. Nothing is inferred from queue activity."
              />
            </dt>
            <dd class="mt-1 text-highlighted">
              {{
                instance.worker_status || "No recent worker activity reported"
              }}
            </dd>
          </div>
        </dl>
        <p class="text-xs text-muted">
          Storage, identity trust, networking, profiles and secret provisioning
          are deployment-managed and cannot be edited here.
        </p>
      </section>
      <p v-else role="alert">
        This administration page or registry does not exist.
      </p>
    </template>
  </main>
</template>
