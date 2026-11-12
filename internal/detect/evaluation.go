package detect

import (
	"sort"

	"github.com/JtheGunner/doc-drift-checker/internal/docparse"
	"github.com/JtheGunner/doc-drift-checker/internal/links"
	"github.com/JtheGunner/doc-drift-checker/internal/relevance"
)

// Mode is the detection mode an evaluation ran in.
type Mode string

// The detection modes.
const (
	ModeDiff    Mode = "diff"
	ModeHistory Mode = "history"
)

// Verdict is the final decision for a section.
type Verdict string

// The verdicts.
const (
	VerdictDrift   Verdict = "drift"
	VerdictNoDrift Verdict = "no-drift"
)

// Reason names the fact that decided the verdict.
type Reason string

// Reasons of a drift verdict.
const (
	// ReasonCodeChanged: a mapped file changed between base and HEAD (after the
	// relevance rules) and none of the section's own lines did.
	ReasonCodeChanged Reason = "code-changed"
	// ReasonCodeNewer: a mapped file's newest commit is newer than the section's.
	ReasonCodeNewer Reason = "code-newer"
	// ReasonLockMismatch: history mode; the mapped code differs from the state
	// acknowledged in the lockfile.
	ReasonLockMismatch Reason = "lock-mismatch"
)

// Reasons of a no-drift verdict.
const (
	ReasonNoMappedCode    Reason = "no-mapped-code"
	ReasonNoFileChanged   Reason = "no-mapped-file-changed"
	ReasonAllIgnored      Reason = "all-changes-ignored"
	ReasonOwnLinesChanged Reason = "own-lines-changed"
	ReasonAcknowledged    Reason = "acknowledged"
	ReasonCodeNotNewer    Reason = "code-not-newer"
	ReasonNoDatedCode     Reason = "no-dated-code"
	ReasonNoBlameData     Reason = "no-blame-data"
)

// LockState is how a section's lockfile entry compares with the mapped code.
type LockState string

// The lock states.
const (
	LockNone     LockState = "none"
	LockMatches  LockState = "matches"
	LockMismatch LockState = "mismatch"
)

// LockEffect is what the lockfile entry did to the section's result.
type LockEffect string

// The lock effects.
const (
	// LockEffectNone: the section has no entry.
	LockEffectNone LockEffect = "none"
	// LockEffectSuppressed: a drift was dropped because the entry matches.
	LockEffectSuppressed LockEffect = "suppressed"
	// LockEffectForcedDrift: history mode; the entry does not match, so the
	// section drifts and lists the differing files.
	LockEffectForcedDrift LockEffect = "forced-drift"
	// LockEffectNoEffect: the entry exists but did not change the result.
	LockEffectNoEffect LockEffect = "no-effect"
)

// CommitRef is a commit as shown in an explanation. The subject is untrusted
// text from the repository and may be empty.
type CommitRef struct {
	Hash string
	// Time is the committer time in unix seconds.
	Time    int64
	Subject string
}

// FileChange is one change between base and HEAD with the relevance decision
// made for it. Status is git's status letter (A, M, D, R, C, T).
type FileChange struct {
	Status   string
	Path     string
	OldPath  string
	Decision relevance.Decision
}

// matches reports whether the change concerns the linked file: its new path, or
// the old path of a rename (a copy leaves its source unchanged).
func (c FileChange) matches(file string) bool {
	return c.Path == file || (c.Status == "R" && c.OldPath == file)
}

// FileCommits are the commits of base..HEAD touching a file, newest first. The
// list is capped; Truncated says more existed.
type FileCommits struct {
	Commits   []CommitRef
	Truncated bool
}

// HistoryFile is what history mode knows about a mapped file. A file matching
// an ignore glob carries it in IgnoredGlob and is not dated. Newest is the
// newest commit that is not in the ignore-revs file; nil when there is none.
type HistoryFile struct {
	IgnoredGlob string
	Newest      *CommitRef
}

// EvalInput is everything an evaluation needs; it is gathered by the caller
// (git and filesystem) and never read here.
type EvalInput struct {
	Mode Mode
	// Base is the base reference of diff mode.
	Base string
	// Docs are the resolved links per documentation file.
	Docs []links.DocLinks
	// IgnoreRules are the configured ignore globs, for display.
	IgnoreRules []string

	// Changes holds every change between base and HEAD with its relevance
	// decision (diff mode). DocChanges maps a documentation file to its changed
	// line ranges; a file without an entry is unchanged.
	Changes    []FileChange
	DocChanges map[string][]LineRange

	// SectionDates is the newest commit blamed for each section's own lines and
	// HistoryFiles the dating of each mapped file by path (history mode).
	SectionDates map[SectionKey]CommitRef
	HistoryFiles map[string]HistoryFile

	// Lock holds the lockfile verdicts, see ApplyLock.
	Lock map[SectionKey][]string

	// Commits are optional details per file path: the commits of base..HEAD.
	Commits map[string]FileCommits
}

// FileEvaluation is how one mapped file entered the decision.
type FileEvaluation struct {
	Path   string
	Origin links.Origin
	// Changes are the changes between base and HEAD that concern the file
	// (diff mode); empty when it did not change.
	Changes []FileChange
	// Commits and CommitsTruncated list the commits of base..HEAD touching the
	// file, when the caller gathered them.
	Commits          []CommitRef
	CommitsTruncated bool
	// History mode: IgnoredGlob is the ignore glob that excluded the file from
	// dating; Newest its newest commit that counts, nil when there is none.
	IgnoredGlob string
	Newest      *CommitRef
	// Triggers reports whether the file made the section drift before the
	// lockfile was applied: a relevant change (diff mode) or a commit strictly
	// newer than the section (history mode).
	Triggers bool
}

// LockEvaluation is the lockfile's part in an evaluation.
type LockEvaluation struct {
	State LockState
	// Differing are the mapped files that differ from the acknowledged state.
	Differing []string
	Effect    LockEffect
	// DriftedBeforeLock reports whether the section drifted before the
	// lockfile was applied.
	DriftedBeforeLock bool
}

// Evaluation records everything the decision for one section was based on. It
// is the single source for both check (which reports the drifted ones) and
// explain (which renders one).
type Evaluation struct {
	// Key is "<doc>#<anchor>", the section key of the lockfile and of ack.
	Key     string
	Doc     string
	Anchor  string
	Heading string
	// Line is the first line of the section; OwnStart and OwnEnd bound its own
	// text (child sections excluded).
	Line     int
	OwnStart int
	OwnEnd   int
	Mode     Mode
	Base     string

	// Links with their origin come from the resolver; Missing and Unmapped
	// describe references and sections without a mapping.
	Missing  []links.Missing
	Unmapped bool
	Files    []FileEvaluation

	// IgnoreRules are the configured ignore globs.
	IgnoreRules []string

	// Diff mode: DocChanged reports whether the document changed at all, and
	// OwnHunks the changed ranges that touch the section's own lines.
	DocChanged bool
	OwnHunks   []LineRange
	// History mode: SectionDate is the newest commit blamed for the section's
	// own lines; nil without blame data.
	SectionDate *CommitRef

	Lock    LockEvaluation
	Verdict Verdict
	Reason  Reason
	// Drift is the reported drift; meaningful only when Verdict is VerdictDrift.
	Drift Drift
}

// Evaluations holds one Evaluation per section of every document, in document
// and section order.
type Evaluations []Evaluation

// Find returns the evaluation of the section with the given key.
func (e Evaluations) Find(key string) (Evaluation, bool) {
	for _, ev := range e {
		if ev.Key == key {
			return ev, true
		}
	}
	return Evaluation{}, false
}

// Drifts returns the drifts of the evaluations that ended as drift, in order.
// This is what check reports.
func (e Evaluations) Drifts() []Drift {
	drifts := []Drift{}
	for _, ev := range e {
		if ev.Verdict == VerdictDrift {
			drifts = append(drifts, ev.Drift)
		}
	}
	return drifts
}

// WithSeverity returns the evaluations with the severity of every drift set by
// Classify; see Classify for refs and versions. The receiver is not modified.
func (e Evaluations) WithSeverity(docs []links.DocLinks, refs map[SectionKey][]docparse.Reference, versions map[string]FileVersion) Evaluations {
	classified := Classify(docs, refs, versions, e.Drifts())
	byKey := make(map[SectionKey]Drift, len(classified))
	for _, d := range classified {
		byKey[SectionKey{Doc: d.Doc, Line: d.Line}] = d
	}
	out := make(Evaluations, len(e))
	copy(out, e)
	for i, ev := range out {
		if ev.Verdict != VerdictDrift {
			continue
		}
		out[i].Drift = byKey[SectionKey{Doc: ev.Doc, Line: ev.Line}]
	}
	return out
}

// SectionID is the section key "<doc>#<anchor>".
func SectionID(doc, anchor string) string {
	return doc + "#" + anchor
}

// Evaluate decides every section of every document and records the facts the
// decision rests on. The decision itself is made by Detect or History followed
// by ApplyLock; the verdict is whether a section survived them. Callers refine
// the severity with WithSeverity.
func Evaluate(in EvalInput) Evaluations {
	history := in.Mode == ModeHistory
	var kept []Change
	var drifts []Drift
	if history {
		drifts = History(HistoryInput{Docs: in.Docs, SectionDates: commitTimes(in.SectionDates), FileDates: fileDates(in.HistoryFiles)})
	} else {
		kept = keptChanges(in.Changes)
		drifts = Detect(Input{Docs: in.Docs, Changes: kept, DocChanges: in.DocChanges})
	}
	beforeLock := make(map[SectionKey]bool, len(drifts))
	for _, d := range drifts {
		beforeLock[SectionKey{Doc: d.Doc, Line: d.Line}] = true
	}
	final := make(map[SectionKey]Drift, len(drifts))
	for _, d := range ApplyLock(in.Docs, drifts, in.Lock, history) {
		final[SectionKey{Doc: d.Doc, Line: d.Line}] = d
	}

	evals := Evaluations{}
	for _, doc := range in.Docs {
		for i, section := range doc.Sections {
			key := SectionKey{Doc: doc.Doc, Line: section.StartLine}
			ev := newEvaluation(in, doc, i)
			ev.Lock = lockEvaluation(in.Lock, key, history, beforeLock[key])
			ev.Files = fileEvaluations(in, section, ev.SectionDate)
			if d, ok := final[key]; ok {
				ev.Verdict, ev.Drift = VerdictDrift, d
				ev.Reason = driftReason(history, ev.Lock.Effect)
			} else {
				ev.Verdict = VerdictNoDrift
				ev.Reason = noDriftReason(history, ev)
			}
			evals = append(evals, ev)
		}
	}
	return evals
}

func newEvaluation(in EvalInput, doc links.DocLinks, i int) Evaluation {
	section := doc.Sections[i]
	start, end := OwnRange(doc.Sections, i)
	ev := Evaluation{
		Key: SectionID(doc.Doc, section.Anchor), Doc: doc.Doc, Anchor: section.Anchor, Heading: section.Heading,
		Line: section.StartLine, OwnStart: start, OwnEnd: end, Mode: in.Mode, Base: in.Base,
		Missing: section.Missing, Unmapped: section.Unmapped, IgnoreRules: in.IgnoreRules,
		OwnHunks: []LineRange{},
	}
	if in.Mode == ModeHistory {
		if date, ok := in.SectionDates[SectionKey{Doc: doc.Doc, Line: section.StartLine}]; ok {
			ev.SectionDate = &date
		}
		return ev
	}
	ranges, changed := in.DocChanges[doc.Doc]
	ev.DocChanged = changed
	for _, r := range ranges {
		if touched(span{start, end}, []LineRange{r}) {
			ev.OwnHunks = append(ev.OwnHunks, r)
		}
	}
	return ev
}

func lockEvaluation(locked map[SectionKey][]string, key SectionKey, history, driftedBefore bool) LockEvaluation {
	differing, hasEntry := locked[key]
	_, effect := decideLock(history, driftedBefore, hasEntry, differing)
	state := LockNone
	switch {
	case !hasEntry:
	case len(differing) == 0:
		state = LockMatches
	default:
		state = LockMismatch
	}
	return LockEvaluation{State: state, Differing: append([]string{}, differing...), Effect: effect, DriftedBeforeLock: driftedBefore}
}

func fileEvaluations(in EvalInput, section links.SectionLinks, sectionDate *CommitRef) []FileEvaluation {
	files := make([]FileEvaluation, 0, len(section.Links))
	seen := map[string]bool{}
	for _, l := range section.Links {
		if seen[l.File] {
			continue
		}
		seen[l.File] = true
		fe := FileEvaluation{Path: l.File, Origin: l.Origin, Changes: []FileChange{}}
		if in.Mode == ModeHistory {
			hf := in.HistoryFiles[l.File]
			fe.IgnoredGlob, fe.Newest = hf.IgnoredGlob, hf.Newest
			fe.Triggers = hf.Newest != nil && sectionDate != nil && hf.Newest.Time > sectionDate.Time
		} else {
			for _, c := range in.Changes {
				if c.matches(l.File) {
					fe.Changes = append(fe.Changes, c)
					fe.Triggers = fe.Triggers || c.Decision.Relevant
				}
			}
			details := in.Commits[l.File]
			fe.Commits, fe.CommitsTruncated = details.Commits, details.Truncated
		}
		files = append(files, fe)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files
}

func driftReason(history bool, effect LockEffect) Reason {
	switch {
	case !history:
		return ReasonCodeChanged
	case effect == LockEffectForcedDrift:
		return ReasonLockMismatch
	}
	return ReasonCodeNewer
}

// noDriftReason names why a section that did not survive the decision is not
// reported. It only reads the recorded facts.
func noDriftReason(history bool, ev Evaluation) Reason {
	switch {
	case ev.Lock.Effect == LockEffectSuppressed:
		return ReasonAcknowledged
	case len(ev.Files) == 0:
		return ReasonNoMappedCode
	case history:
		return historyNoDriftReason(ev)
	}
	for _, f := range ev.Files {
		if f.Triggers {
			return ReasonOwnLinesChanged
		}
	}
	for _, f := range ev.Files {
		if len(f.Changes) > 0 {
			return ReasonAllIgnored
		}
	}
	return ReasonNoFileChanged
}

func historyNoDriftReason(ev Evaluation) Reason {
	dated := false
	for _, f := range ev.Files {
		dated = dated || f.Newest != nil
	}
	switch {
	case !dated:
		return ReasonNoDatedCode
	case ev.SectionDate == nil:
		return ReasonNoBlameData
	}
	return ReasonCodeNotNewer
}

// keptChanges converts the relevant changes for Detect. Only renames keep their
// old path, since a copy leaves the source file unchanged.
func keptChanges(changes []FileChange) []Change {
	out := make([]Change, 0, len(changes))
	for _, c := range changes {
		if !c.Decision.Relevant {
			continue
		}
		dc := Change{Path: c.Path}
		if c.Status == "R" {
			dc.OldPath = c.OldPath
		}
		out = append(out, dc)
	}
	return out
}

func commitTimes(dates map[SectionKey]CommitRef) map[SectionKey]int64 {
	out := make(map[SectionKey]int64, len(dates))
	for k, c := range dates {
		out[k] = c.Time
	}
	return out
}

func fileDates(files map[string]HistoryFile) map[string]int64 {
	out := make(map[string]int64, len(files))
	for path, f := range files {
		if f.Newest != nil {
			out[path] = f.Newest.Time
		}
	}
	return out
}
