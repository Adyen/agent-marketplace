import type { AdminProposal } from "~/lib/admin";

export type BadgeColor =
  | "primary"
  | "secondary"
  | "success"
  | "info"
  | "warning"
  | "error"
  | "neutral";
export type Badge = { label: string; color: BadgeColor };

const RELATIVE_UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ["year", 31_536_000],
  ["month", 2_592_000],
  ["day", 86_400],
  ["hour", 3_600],
  ["minute", 60],
];

// Human relative time for a stored UTC timestamp. Returns the raw value when
// unparsable so nothing is silently hidden.
export function relativeAdminTime(
  iso: string | undefined,
  now: Date = new Date(),
): string {
  if (!iso) return "Unknown time";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  const seconds = Math.round((date.getTime() - now.getTime()) / 1000);
  if (Math.abs(seconds) < 45) return "just now";
  const format = new Intl.RelativeTimeFormat("en", { numeric: "auto" });
  for (const [unit, size] of RELATIVE_UNITS) {
    if (Math.abs(seconds) >= size)
      return format.format(Math.round(seconds / size), unit);
  }
  return format.format(Math.round(seconds / 60), "minute");
}

export function absoluteAdminTime(iso: string | undefined): string {
  if (!iso) return "Unknown time";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return `${date.toISOString().slice(0, 19).replace("T", " ")} UTC`;
}

// UUIDs and hash-suffixed identifiers (managed-refresh-<sha256>) keep a
// readable prefix plus the first 8 hex characters; other IDs stay intact.
// The full value is always available for copying.
export function shortId(value: string | undefined): string {
  if (!value) return "";
  if (
    /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(
      value,
    )
  )
    return value.slice(0, 8);
  const hashed = /^(.*?)([0-9a-f]{32,})$/i.exec(value);
  return hashed ? `${hashed[1]}${hashed[2]!.slice(0, 8)}` : value;
}

const LIFECYCLE: Record<string, Badge> = {
  active: { label: "Active", color: "success" },
  paused: { label: "Paused", color: "warning" },
  withdrawn: { label: "Withdrawn", color: "error" },
  retired: { label: "Retired", color: "neutral" },
};
const OUTCOME: Record<string, Badge> = {
  succeeded: { label: "Succeeded", color: "success" },
  completed: { label: "Completed", color: "success" },
  failed: { label: "Failed", color: "error" },
  started: { label: "Started", color: "info" },
  pending: { label: "Pending", color: "neutral" },
  queued: { label: "Queued", color: "neutral" },
  leased: { label: "Leased", color: "info" },
  running: { label: "Running", color: "info" },
  cancelled: { label: "Cancelled", color: "neutral" },
  skipped: { label: "Skipped", color: "neutral" },
};

function fallbackBadge(value: string | undefined): Badge {
  if (!value) return { label: "Unknown", color: "neutral" };
  return {
    label: value.charAt(0).toUpperCase() + value.slice(1).replaceAll("_", " "),
    color: "neutral",
  };
}
export function lifecycleBadge(state: string | undefined): Badge {
  return LIFECYCLE[state || ""] || fallbackBadge(state);
}
export function outcomeBadge(outcome: string | undefined): Badge {
  return OUTCOME[outcome || ""] || fallbackBadge(outcome);
}
export const jobStatusBadge = outcomeBadge;

const CHANGE: Record<string, Badge> = {
  added: { label: "New", color: "success" },
  changed: { label: "Changed", color: "info" },
  unchanged: { label: "Unchanged", color: "neutral" },
  removed: { label: "Removed", color: "error" },
};
const DECISION: Record<string, Badge> = {
  allow: { label: "Allowed", color: "success" },
  warn: { label: "Warnings", color: "warning" },
  reject: { label: "Rejected", color: "error" },
};
export function changeBadge(change: string | undefined): Badge {
  return CHANGE[change || ""] || fallbackBadge(change);
}
export function decisionBadge(decision: string | undefined): Badge {
  return DECISION[decision || ""] || fallbackBadge(decision);
}

// Preview failure codes come from the worker; the message names the offending
// root or resource. These titles explain the class of problem and what to try.
export const previewFailureCopy: Record<
  string,
  { title: string; hint: string }
> = {
  acquisition_failed: {
    title: "The repository could not be acquired",
    hint: "Check the origin URL, the ref and the credential profile. Git output is not shown because it can contain credentials.",
  },
  profile_unavailable: {
    title: "Origin or profile not permitted",
    hint: "The deployment does not allow this origin, or the selected profile does not cover it.",
  },
  root_missing: {
    title: "A source root does not exist at this ref",
    hint: "Check the spelling and that the directory is committed on the selected ref.",
  },
  catalog_error: {
    title: "The registry catalog could not be built",
    hint: "Check the diagnostic below. Malformed skills are skipped when valid resources remain; a missing layout or registry-wide error still blocks indexing.",
  },
  duplicate_resource: {
    title: "Two roots expose the same resource",
    hint: "Narrow the overlapping roots or rename one of the resources in the registry.",
  },
  timeout: {
    title: "The preview ran out of time",
    hint: "The repository may be too large for the deployment limits. Try a narrower root.",
  },
  discovery_failed: {
    title: "Discovery failed",
    hint: "The message below names the resource or root that could not be processed.",
  },
};
export function previewFailureTitle(code: string | undefined): {
  title: string;
  hint: string;
} {
  return (
    previewFailureCopy[code || ""] || {
      title: "The preview could not be completed",
      hint: "Indexing would most likely fail the same way. Fix the reported problem and preview again.",
    }
  );
}

export function previewKindCounts(
  resources: readonly { kind: string }[],
): [string, number][] {
  const counts = new Map<string, number>();
  for (const item of resources)
    counts.set(item.kind, (counts.get(item.kind) || 0) + 1);
  return [...counts.entries()].sort(
    (a, b) => b[1] - a[1] || a[0].localeCompare(b[0]),
  );
}

// Which lifecycle actions are meaningful for a registry in its current state.
// The backend still validates; this only avoids showing obviously inapplicable buttons.
export function applicableLifecycleActions(registry: {
  paused: boolean;
  withdrawn: boolean;
  retired: boolean;
}): AdminProposal["action"][] {
  if (registry.retired) return ["restore_retired"];
  if (registry.withdrawn) return ["restore", "retire"];
  return [registry.paused ? "resume" : "pause", "reindex", "withdraw"];
}
export const destructiveLifecycleActions: ReadonlySet<AdminProposal["action"]> =
  new Set(["withdraw", "retire"]);

export const operationLabels: Record<string, string> = {
  upsert_registry: "Registry settings changed",
  set_policy: "License policy changed",
  pause: "Refresh paused",
  resume: "Refresh resumed",
  withdraw: "Availability withdrawn",
  restore: "Availability restored",
  retire: "Registry retired",
  restore_retired: "Retired registry restored",
  reindex: "Reindex requested",
  "configuration.adopt": "Configuration adopted",
};
export function operationLabel(operation: string | undefined): string {
  return operationLabels[operation || ""] || fallbackBadge(operation).label;
}
