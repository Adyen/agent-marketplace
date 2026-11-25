import fuzzysort from 'fuzzysort'
import type { Row } from './controller'

export type SearchMatch = { row: Row; nameMatch: boolean; score: number; indexes: number[] }
export type NameSegment = { text: string; matched: boolean }

export function searchTerms(query: string): string[] {
  return query.toLowerCase().match(/[\p{L}\p{N}]+/gu) ?? []
}

export function hasSearchTerms(query: string): boolean {
  return searchTerms(query).length > 0
}

export function compareRows(left: Row, right: Row): number {
  if (left.name < right.name) {
    return -1
  }
  if (left.name > right.name) {
    return 1
  }
  if (left.id < right.id) {
    return -1
  }
  if (left.id > right.id) {
    return 1
  }
  return 0
}

export function compareSearchMatches(left: SearchMatch, right: SearchMatch): number {
  if (left.nameMatch !== right.nameMatch) {
    return left.nameMatch ? -1 : 1
  }
  if (left.score !== right.score) {
    return right.score - left.score
  }
  return compareRows(left.row, right.row)
}

export function rankRows(rows: Row[], query: string): SearchMatch[] {
  const terms = searchTerms(query)
  if (terms.length === 0) {
    return rows.map((row) => ({ row, nameMatch: false, score: 0, indexes: [] }))
  }

  const needle = terms.join('')
  const matches: SearchMatch[] = []
  for (const row of rows) {
    const nameResult = fuzzysort.single(needle, row.searchName)
    if (nameResult != null) {
      matches.push({
        row,
        nameMatch: true,
        score: nameResult.score,
        indexes: [...nameResult.indexes],
      })
      continue
    }
    if (terms.every((term) => row.searchWords.some((word) => word.startsWith(term)))) {
      matches.push({ row, nameMatch: false, score: 0, indexes: [] })
    }
  }
  return matches.sort(compareSearchMatches)
}

export function highlightNameParts(displayName: string, searchName: string, indexes: number[]): NameSegment[] {
  if (indexes.length === 0) {
    return [{ text: displayName, matched: false }]
  }

  const displayCharacters = Array.from(displayName)
  const searchCharacters = Array.from(searchName)
  if (displayCharacters.length !== searchCharacters.length) {
    return [{ text: displayName, matched: false }]
  }

  const matchingOffsets = new Set(indexes)
  const parts: NameSegment[] = []
  let searchOffset = 0
  for (let index = 0; index < displayCharacters.length; index++) {
    const searchCharacter = searchCharacters[index]
    let matched = false
    for (let offset = 0; offset < searchCharacter.length; offset++) {
      if (matchingOffsets.has(searchOffset + offset)) {
        matched = true
        break
      }
    }
    const previous = parts[parts.length - 1]
    if (previous != null && previous.matched === matched) {
      previous.text += displayCharacters[index]
    } else {
      parts.push({ text: displayCharacters[index], matched })
    }
    searchOffset += searchCharacter.length
  }
  return parts
}
