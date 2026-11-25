export interface SourceFailure {
  summary: string
  detail: string
}

const SUMMARIES: [RegExp, string][] = [
  [/generation conflict/i, 'Your configuration changed while the source was being added. Try again.'],
  [/authentication failed|permission denied|could not read username|terminal prompts disabled/i, "Couldn't sign in to the repository. Check that you have access to it."],
  [/repository .* not found|not appear to be a git repository|could not resolve host|unable to access|determine default branch/i, "Couldn't reach the repository. Check the URL and your network connection."],
  [/couldn't find remote ref|remote ref .* not found|unknown revision|did not advertise/i, "The branch, tag or commit wasn't found in the repository."],
  [/not allowed|allowlist|allowed_registries|allowed_marketplaces/i, "Your organization doesn't allow this source."],
  [/no such file or directory|does not exist|not a directory/i, "The folder wasn't found."],
]

// describeSourceFailure turns a raw add-source error into one plain sentence
// and keeps the original text as detail for troubleshooting.
export function describeSourceFailure(message: string, fallback = "The source couldn't be added."): SourceFailure {
  const summary = SUMMARIES.find(([pattern]) => pattern.test(message))?.[1] ?? fallback
  return { summary, detail: message }
}
