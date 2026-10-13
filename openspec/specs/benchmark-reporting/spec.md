# benchmark-reporting Specification

## Purpose
Provide concise benchmark comparisons that distinguish observed effects from statistical evidence and do not claim a PR caused a performance change.

## Requirements

### Requirement: Significance accounts for multiple comparisons

The benchmark report SHALL apply Holm correction across every comparable benchmark metric in one report before treating a difference as significant. The family MUST include time, bytes, and allocation tests even when unchanged or below the notable threshold, and MUST exclude benchmarks present on only one side. A notable difference MUST satisfy adjusted p<0.05 and an absolute median change of at least 10%. The report SHALL count notable metric increases and decreases separately without classifying them as confirmed regressions or improvements.

#### Scenario: Weak evidence is not promoted
- **WHEN** a report contains differences that meet the unadjusted significance and effect thresholds but fail the report-wide correction
- **THEN** those differences MUST NOT appear as notable changes
- **AND** the report MUST expose their corrected evidence in collapsed details without claiming equivalence

#### Scenario: Strong evidence remains highlighted
- **WHEN** an observed metric increase or decrease passes the corrected significance and effect thresholds
- **THEN** the report MUST include its observed direction and magnitude in the compact notable table
- **AND** added or removed benchmarks MUST remain identified without significance claims

#### Scenario: Mixed metric directions remain visible
- **WHEN** one benchmark has a qualifying time increase and qualifying memory decrease
- **THEN** the report MUST count and show both directions rather than assigning one regression or improvement label

### Requirement: Statistical details do not overwhelm the summary

The report SHALL keep sample counts, adjusted p-values, and statistical method explanations inside collapsed details. The expanded summary MUST include a short runner-conditions caveat. Details MUST state that significance does not establish PR causation and that insufficient evidence does not establish unchanged performance.

#### Scenario: Technical evidence stays collapsed
- **WHEN** a report is rendered
- **THEN** the expanded section MUST show concise direction counts and notable effects without statistical-method or p-value details
- **AND** collapsed details MUST show base/head sample counts, adjusted p-values for each comparable metric, and the evidence limitations
