// Package scanner keeps the library in sync with the file system.
//
// Jellyfin problems it addresses:
//   - slow scans on big libraries: an unchanged file costs one stat(); only new
//     or changed files are probed, by a bounded worker pool, with one short
//     write transaction per file so browsing never blocks;
//   - lost watch state after renaming/moving files: a vanished file + a new file
//     of identical size is treated as a move and keeps its id and user data;
//   - libraries "disappearing" when an SMB/NFS share drops: an unreachable or
//     suddenly empty root aborts the scan without touching anything, and vanished
//     files are only flagged missing=1, never deleted.
//
// Inotify is deliberately not used (unreliable on network shares); scans run at
// startup, periodically, and on demand.
package scanner

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ScotyLucas/kinora/internal/media"
	"github.com/ScotyLucas/kinora/internal/safe"
	"github.com/ScotyLucas/kinora/internal/store"
)

// VideoExts are the recognized video file extensions.
var VideoExts = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".mov": true, ".webm": true, ".ts": true,
	".m2ts": true, ".wmv": true, ".flv": true, ".mpg": true, ".mpeg": true, ".ogv": true,
}

var subtitleExts = map[string]bool{".srt": true, ".vtt": true, ".ass": true, ".ssa": true}

var skipDirs = map[string]bool{"@eadir": true, "#recycle": true, "trailers": true, "featurettes": true,
	"behind the scenes": true, "$recycle.bin": true, "lost+found": true, "#snapshot": true}

// Status is the live progress of a library scan.
type Status struct {
	LibraryID int64  `json:"library_id"`
	Running   bool   `json:"running"`
	Phase     string `json:"phase"` // walk | probe | cleanup | done
	Total     int    `json:"total"`
	Done      int    `json:"done"`
	Changed   int    `json:"changed"`
	Moved     int    `json:"moved"`
	Removed   int    `json:"removed"`
	Errors    int    `json:"errors"`
	TookMs    int64  `json:"took_ms"`
	Error     string `json:"error,omitempty"`
	LastError string `json:"last_error,omitempty"` // last per-file problem (probe failure…)
	StartedAt int64  `json:"started_at"`
}

// Scanner scans libraries.
type Scanner struct {
	st     *store.Store
	imgDir string
	log    *slog.Logger

	// OnScanned is called after a successful library scan (e.g. to fetch metadata).
	OnScanned func(libID int64)

	mu      sync.Mutex
	status  map[int64]*Status
	cancels map[int64]context.CancelFunc
	wg      sync.WaitGroup
}

// New creates a scanner writing artwork below imgDir.
func New(st *store.Store, imgDir string, log *slog.Logger) *Scanner {
	return &Scanner{st: st, imgDir: imgDir, log: log, status: map[int64]*Status{}, cancels: map[int64]context.CancelFunc{}}
}

// Statuses returns a snapshot of every library's scan state.
func (sc *Scanner) Statuses() []Status {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	out := make([]Status, 0, len(sc.status))
	for _, s := range sc.status {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LibraryID < out[j].LibraryID })
	return out
}

// Running reports how many scans are in progress.
func (sc *Scanner) Running() int {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return len(sc.cancels)
}

func (sc *Scanner) update(id int64, f func(s *Status)) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if s := sc.status[id]; s != nil {
		f(s)
	}
}

// Trigger starts a background scan; false if one is already running for that library.
func (sc *Scanner) Trigger(ctx context.Context, libID int64) bool {
	sc.mu.Lock()
	if _, busy := sc.cancels[libID]; busy {
		sc.mu.Unlock()
		return false
	}
	cctx, cancel := context.WithCancel(ctx)
	sc.cancels[libID] = cancel
	sc.status[libID] = &Status{LibraryID: libID, Running: true, Phase: "walk", StartedAt: time.Now().Unix()}
	sc.wg.Add(1)
	sc.mu.Unlock()
	go func() {
		defer sc.wg.Done()
		err := safe.Call(func() error { return sc.scan(cctx, libID) })
		var pe *safe.PanicError
		if errors.As(err, &pe) {
			sc.log.Error("scan panicked", "library", libID, "err", pe, "stack", string(pe.Stack))
		}
		sc.mu.Lock()
		delete(sc.cancels, libID)
		if s := sc.status[libID]; s != nil {
			s.Running = false
			s.Phase = "done"
			if err != nil {
				s.Error = err.Error()
			}
			s.TookMs = time.Now().UnixMilli() - s.StartedAt*1000
		}
		sc.mu.Unlock()
		cancel()
		if err != nil {
			sc.log.Warn("scan failed", "library", libID, "err", err)
		} else if sc.OnScanned != nil {
			sc.OnScanned(libID)
		}
	}()
	return true
}

// ScanNow runs a scan synchronously (used by tests).
func (sc *Scanner) ScanNow(ctx context.Context, libID int64) (Status, error) {
	if !sc.Trigger(ctx, libID) {
		return Status{}, errors.New("már fut szkennelés")
	}
	for {
		time.Sleep(20 * time.Millisecond)
		sc.mu.Lock()
		_, busy := sc.cancels[libID]
		s := *sc.status[libID]
		sc.mu.Unlock()
		if !busy {
			if s.Error != "" {
				return s, errors.New(s.Error)
			}
			return s, nil
		}
	}
}

// Cancel stops a running scan of a library (e.g. before deleting it).
func (sc *Scanner) Cancel(libID int64) {
	sc.mu.Lock()
	c := sc.cancels[libID]
	sc.mu.Unlock()
	if c != nil {
		c()
	}
}

// Forget drops the status entry of a deleted library.
func (sc *Scanner) Forget(libID int64) {
	sc.mu.Lock()
	delete(sc.status, libID)
	sc.mu.Unlock()
}

// Wait blocks until all scans have finished.
func (sc *Scanner) Wait() { sc.wg.Wait() }

// Run scans all libraries at startup and then every scan_interval_min minutes.
func (sc *Scanner) Run(ctx context.Context) {
	scanAll := func() {
		libs, err := sc.st.Libraries()
		if err != nil {
			sc.log.Error("list libraries", "err", err)
			return
		}
		for _, l := range libs {
			sc.Trigger(ctx, l.ID)
		}
	}
	scanAll()
	last := time.Now()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			iv := sc.st.SettingInt("scan_interval_min", 15)
			if iv > 0 && time.Since(last) >= time.Duration(iv)*time.Minute {
				last = time.Now()
				scanAll()
			}
		}
	}
}

// ------------------------------------------------------------------ the scan

type fileEntry struct {
	abs, rel string
	root     string // the library folder the file was found in
	size     int64
	mtime    int64
	sig      string
	parsed   Parsed
	dir      string
	base     string // file name without extension
	listing  dirListing
	solo     bool // only video file in its folder (folder artwork/NFO belongs to it)
	known    *store.KnownFile
}

type seriesEntry struct {
	key, title string
	year       int
	dirAbs     string
	sig        string
	seasons    map[int]string // season → season folder abs path ("" if none)
}

// ErrRootUnavailable is reported when a library root cannot be read.
var ErrRootUnavailable = errors.New("a könyvtár gyökérmappája nem érhető el – a szkennelés leállt, semmi nem lett törölve")

func (sc *Scanner) scan(ctx context.Context, libID int64) error {
	lib, err := sc.st.Library(libID)
	if err != nil {
		return err
	}
	roots := make([]string, 0, len(lib.Paths))
	for _, p := range lib.Paths {
		root := filepath.Clean(p)
		if _, err := os.ReadDir(root); err != nil {
			return fmt.Errorf("%w (%s: %v)", ErrRootUnavailable, root, err)
		}
		roots = append(roots, root)
	}
	known, err := sc.st.KnownFiles(libID)
	if err != nil {
		return err
	}
	containers, err := sc.st.KnownContainers(libID)
	if err != nil {
		return err
	}

	// ---- phase 1: walk (stat only)
	var files []*fileEntry
	var unreadable []string
	videosPerDir := map[string]int{}
	perRoot := map[string]int{}
	for _, root := range roots {
		walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				if d != nil && d.IsDir() || p == root {
					unreadable = append(unreadable, p)
					return fs.SkipDir
				}
				return nil
			}
			name := d.Name()
			if d.IsDir() {
				if p != root && (strings.HasPrefix(name, ".") || skipDirs[strings.ToLower(name)]) {
					return fs.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(name))
			if !VideoExts[ext] || strings.HasPrefix(name, ".") {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if isSampleOrTrailer(name, info.Size()) {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			dir := filepath.Dir(p)
			videosPerDir[dir]++
			files = append(files, &fileEntry{abs: p, rel: rel, root: root, size: info.Size(), mtime: info.ModTime().Unix(), dir: dir,
				base: strings.TrimSuffix(name, filepath.Ext(name))})
			perRoot[root]++
			sc.update(libID, func(s *Status) { s.Total = len(files) })
			return nil
		})
		if walkErr != nil {
			return walkErr
		}
		for _, u := range unreadable {
			if u == root {
				return fmt.Errorf("%w (%s)", ErrRootUnavailable, root)
			}
		}
	}
	// a folder that is suddenly empty (an unmounted share) aborts the scan without touching anything
	if len(files) == 0 {
		present := 0
		for _, k := range known {
			if !k.Missing {
				present++
			}
		}
		if present > 0 {
			return fmt.Errorf("a(z) %s mappa üres, pedig %d ismert fájl volt benne – lehet, hogy a hálózati megosztás nincs csatolva; semmit nem jelöltem hiányzónak", strings.Join(roots, ", "), present)
		}
	}
	for _, root := range roots {
		if perRoot[root] > 0 {
			continue
		}
		present := 0
		for p, k := range known {
			if !k.Missing && underAny(p, []string{root}) {
				present++
			}
		}
		if present > 0 {
			return fmt.Errorf("a(z) %s mappa üres, pedig %d ismert fájl volt benne – lehet, hogy a hálózati megosztás nincs csatolva; semmit nem jelöltem hiányzónak", root, present)
		}
	}

	// listings & sidecar signatures (one ReadDir per folder that has videos)
	listings := map[string]dirListing{}
	for _, f := range files {
		l, ok := listings[f.dir]
		if !ok {
			l = listDir(f.dir)
			listings[f.dir] = l
		}
		f.listing = l
		f.solo = videosPerDir[f.dir] == 1
		f.parsed = ParseFile(f.rel, lib.Kind)
	}
	// a series added as a movies library: two or more S01E02-named files of the same
	// series become series → season → episodes instead of separate "films"
	if lib.Kind == "movies" {
		alt := map[*fileEntry]Parsed{}
		perSeries := map[string]int{}
		for _, f := range files {
			if p, ok := EpisodeInMoviesLibrary(f.rel); ok {
				alt[f] = p
				perSeries[p.SeriesKey]++
			}
		}
		for f, p := range alt {
			if perSeries[p.SeriesKey] >= 2 {
				f.parsed = p
			}
		}
	}
	for _, f := range files {
		f.sig = fileSig(f)
	}

	// ---- move detection & change set
	seen := map[string]bool{}
	for _, f := range files {
		seen[f.abs] = true
		f.known = known[f.abs]
	}
	vanished := map[int64]*store.KnownFile{}
	bySize := map[int64][]*store.KnownFile{}
	for p, k := range known {
		if !seen[p] && !underAny(p, unreadable) {
			vanished[k.ID] = k
			bySize[k.Size] = append(bySize[k.Size], k)
		}
	}
	moved := 0
	for _, f := range files {
		if f.known != nil {
			continue
		}
		cands := bySize[f.size]
		var pick *store.KnownFile
		for _, c := range cands {
			if _, still := vanished[c.ID]; !still {
				continue
			}
			if pick == nil || filepath.Base(c.Path) == filepath.Base(f.abs) {
				pick = c
			}
		}
		if pick != nil {
			delete(vanished, pick.ID)
			f.known = &store.KnownFile{ID: pick.ID} // force re-save with the old id, new path
			moved++
			sc.log.Info("file moved", "id", pick.ID, "from", pick.Path, "to", f.abs)
		}
	}

	var changed []*fileEntry
	unchanged := 0
	for _, f := range files {
		k := f.known
		if k == nil || k.Path != f.abs || k.Size != f.size || k.Mtime != f.mtime || k.Sig != f.sig || k.Missing || k.Kind != f.parsed.Kind {
			changed = append(changed, f)
		} else {
			unchanged++
			if f.parsed.Kind == "movie" && f.parsed.ImdbID != "" {
				sc.st.SetImdbID(k.ID, f.parsed.ImdbID) // files scanned before Kinora read id tags
			}
		}
	}
	sc.update(libID, func(s *Status) {
		s.Phase = "probe"
		s.Total = len(changed)
		s.Moved = moved
	})

	// ---- series / seasons (serial: cheap, avoids races between workers)
	series := map[string]*seriesEntry{}
	for _, f := range files {
		if f.parsed.Kind != "episode" {
			continue
		}
		se := series[f.parsed.SeriesKey]
		if se == nil {
			se = &seriesEntry{key: f.parsed.SeriesKey, title: f.parsed.Series, year: f.parsed.SeriesYear, seasons: map[int]string{}}
			if f.parsed.SeriesDir != "" {
				se.dirAbs = filepath.Join(f.root, filepath.FromSlash(f.parsed.SeriesDir))
			}
			series[se.key] = se
		}
		if _, ok := se.seasons[f.parsed.Season]; !ok || se.seasons[f.parsed.Season] == "" {
			sd := ""
			if se.dirAbs != "" && f.dir != se.dirAbs {
				sd = f.dir
			}
			se.seasons[f.parsed.Season] = sd
		}
	}
	containerIDs := map[string]int64{}
	for _, se := range series {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		se.sig = seriesSig(se)
		id, err := sc.ensureSeries(ctx, libID, se, containers)
		if err != nil {
			return err
		}
		containerIDs[se.key] = id
		for num, sdir := range se.seasons {
			key := fmt.Sprintf("%s/s%d", se.key, num)
			sid, err := sc.ensureSeason(ctx, libID, id, se, num, sdir, key, containers)
			if err != nil {
				return err
			}
			containerIDs[key] = sid
		}
	}

	sc.joinSameShow(ctx, libID, series, containerIDs, containers)

	// Episodes whose series or season is now another container (an earlier parse split one
	// show into a series per release folder) are processed again, so they move over.
	queued := make(map[*fileEntry]bool, len(changed))
	for _, f := range changed {
		queued[f] = true
	}
	relinked := 0
	for _, f := range files {
		k := f.known
		if f.parsed.Kind != "episode" || k == nil || queued[f] {
			continue
		}
		sid := containerIDs[f.parsed.SeriesKey]
		pid := containerIDs[fmt.Sprintf("%s/s%d", f.parsed.SeriesKey, f.parsed.Season)]
		if k.SeriesID != sid || k.ParentID != pid {
			changed = append(changed, f)
			relinked++
		}
	}
	if relinked > 0 {
		sc.update(libID, func(s *Status) { s.Total = len(changed) })
	}

	// ---- phase 2: probe changed files with a bounded pool
	workers := min(runtime.NumCPU(), 6)
	jobs := make(chan *fileEntry)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				err := safe.Call(func() error { return sc.processFile(ctx, lib, f, containerIDs) })
				sc.update(libID, func(s *Status) {
					s.Done++
					if err != nil {
						s.Errors++
						s.LastError = filepath.Base(f.abs) + ": " + err.Error()
					} else {
						s.Changed++
					}
				})
				if err != nil {
					sc.log.Warn("scan file", "path", f.abs, "err", err)
				}
			}
		}()
	}
feed:
	for _, f := range changed {
		select {
		case jobs <- f:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// ---- phase 3: cleanup (flag, never delete)
	sc.update(libID, func(s *Status) { s.Phase = "cleanup" })
	var gone []int64
	for id, k := range vanished {
		if !k.Missing {
			gone = append(gone, id)
		}
	}
	if err := sc.st.MarkMissing(gone); err != nil {
		return err
	}
	if err := sc.st.FinishScan(libID); err != nil {
		return err
	}
	sc.st.TouchLibraryScan(libID)
	sc.update(libID, func(s *Status) { s.Removed = len(gone) })
	sc.log.Info("scan finished", "library", lib.Name, "files", len(files), "unchanged", unchanged,
		"changed", len(changed), "moved", moved, "missing", len(gone))
	return nil
}

func isSampleOrTrailer(name string, size int64) bool {
	if size >= 300<<20 {
		return false
	}
	l := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	if strings.HasSuffix(l, "-trailer") || strings.HasSuffix(l, ".trailer") || strings.HasSuffix(l, "-sample") {
		return true
	}
	for _, t := range tokenize(l) {
		if t == "sample" {
			return true
		}
	}
	return false
}

func underAny(p string, dirs []string) bool {
	for _, d := range dirs {
		if strings.HasPrefix(p, d+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// fileSig fingerprints the sidecar files that belong to a video (subtitles,
// NFO, artwork), so adding a subtitle re-processes the video without a re-probe
// of everything else.
func fileSig(f *fileEntry) string {
	h := fnv.New64a()
	lb := strings.ToLower(f.base)
	names := make([]string, 0, 8)
	for low, real := range f.listing {
		ext := filepath.Ext(low)
		if VideoExts[ext] {
			continue
		}
		own := strings.HasPrefix(low, lb+".") || strings.HasPrefix(low, lb+"-")
		generic := f.solo && f.parsed.Kind == "movie" && isGenericSidecar(low)
		if own || generic {
			names = append(names, real)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		if st, err := os.Stat(filepath.Join(f.dir, n)); err == nil {
			fmt.Fprintf(h, "%s|%d|%d;", n, st.Size(), st.ModTime().Unix())
		}
	}
	return fmt.Sprintf("%x", h.Sum64())
}

func isGenericSidecar(low string) bool {
	if low == "movie.nfo" {
		return true
	}
	base := strings.TrimSuffix(low, filepath.Ext(low))
	for _, n := range append(append([]string{}, posterNames...), backdropNames...) {
		if base == n {
			return true
		}
	}
	return false
}

func seriesSig(se *seriesEntry) string {
	if se.dirAbs == "" {
		return ""
	}
	h := fnv.New64a()
	var paths []string
	add := func(dir string) {
		for low, real := range listDir(dir) {
			ext := filepath.Ext(low)
			if low == "tvshow.nfo" || (contains(imageExts, ext) && !VideoExts[ext]) {
				paths = append(paths, filepath.Join(dir, real))
			}
		}
	}
	add(se.dirAbs)
	for _, sd := range se.seasons {
		if sd != "" {
			add(sd)
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil {
			fmt.Fprintf(h, "%s|%d|%d;", p, st.Size(), st.ModTime().Unix())
		}
	}
	return fmt.Sprintf("%x", h.Sum64())
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (sc *Scanner) ensureSeries(ctx context.Context, libID int64, se *seriesEntry, known map[string]*store.KnownContainer) (int64, error) {
	if k := known[se.key]; k != nil && k.Sig == se.sig {
		return k.ID, nil
	}
	rec := &store.ContainerRecord{LibraryID: libID, Kind: "series", Key: se.key, Title: se.title, Year: se.year,
		Path: se.dirAbs, Sig: se.sig}
	var listing dirListing
	if se.dirAbs != "" {
		listing = listDir(se.dirAbs)
		if real, ok := listing["tvshow.nfo"]; ok {
			rec.Meta = ReadNFO(filepath.Join(se.dirAbs, real))
		}
	}
	id, err := sc.st.UpsertContainer(rec, SortTitle)
	if err != nil {
		return 0, err
	}
	if se.dirAbs != "" {
		poster := listing.find(se.dirAbs, posterNames...)
		backdrop := listing.find(se.dirAbs, backdropNames...)
		sc.storeArtwork(ctx, id, poster, backdrop)
	}
	return id, nil
}

func (sc *Scanner) ensureSeason(ctx context.Context, libID, seriesID int64, se *seriesEntry, num int, seasonDir, key string, known map[string]*store.KnownContainer) (int64, error) {
	sig := se.sig
	if k := known[key]; k != nil && k.Sig == sig {
		return k.ID, nil
	}
	title := fmt.Sprintf("%d. évad", num)
	if num == 0 {
		title = "Különkiadások"
	}
	n := num
	rec := &store.ContainerRecord{LibraryID: libID, Kind: "season", Key: key, Title: title, ParentID: &seriesID,
		SeriesID: &seriesID, SeasonNum: &n, Path: seasonDir, Sig: sig}
	id, err := sc.st.UpsertContainer(rec, SortTitle)
	if err != nil {
		return 0, err
	}
	poster := ""
	if se.dirAbs != "" {
		l := listDir(se.dirAbs)
		names := []string{fmt.Sprintf("season%02d-poster", num), fmt.Sprintf("season%d-poster", num)}
		if num == 0 {
			names = append(names, "season-specials-poster")
		}
		poster = l.find(se.dirAbs, names...)
	}
	if poster == "" && seasonDir != "" {
		poster = listDir(seasonDir).find(seasonDir, posterNames...)
	}
	sc.storeArtwork(ctx, id, poster, "")
	return id, nil
}

// storeArtwork resizes local images into the image cache and records flags.
// joinSameShow points every copy of a show at one series: folders that metadata matched
// to the same TMDB show (a downloaded release folder and its copy imported into the show's
// folder, or the same show under two names) become one card, as versions of a film do.
// The series with its own folder wins, else the oldest; the others empty and go missing.
func (sc *Scanner) joinSameShow(ctx context.Context, libID int64, series map[string]*seriesEntry, ids map[string]int64, known map[string]*store.KnownContainer) {
	tmdb := sc.st.SeriesTmdbIDsOf(libID)
	groups := map[int64][]*seriesEntry{}
	for _, se := range series {
		if t := tmdb[ids[se.key]]; t > 0 {
			groups[t] = append(groups[t], se)
		}
	}
	for _, g := range groups {
		if len(g) < 2 {
			continue
		}
		lead := g[0]
		for _, se := range g[1:] {
			if (se.dirAbs != "") != (lead.dirAbs != "") {
				if se.dirAbs != "" {
					lead = se
				}
			} else if ids[se.key] < ids[lead.key] {
				lead = se
			}
		}
		for _, se := range g {
			if se == lead {
				continue
			}
			ids[se.key] = ids[lead.key]
			for num := range se.seasons {
				key := fmt.Sprintf("%s/s%d", lead.key, num)
				if _, ok := ids[key]; !ok {
					sid, err := sc.ensureSeason(ctx, libID, ids[lead.key], lead, num, "", key, known)
					if err != nil {
						sc.log.Warn("join show", "series", lead.title, "err", err)
						continue
					}
					ids[key] = sid
				}
				ids[fmt.Sprintf("%s/s%d", se.key, num)] = ids[key]
			}
		}
	}
}

func (sc *Scanner) storeArtwork(ctx context.Context, id int64, poster, backdrop string) {
	dir := ImageDir(sc.imgDir, id)
	hasP, hasB := false, false
	if poster != "" {
		if err := resizeImage(ctx, poster, filepath.Join(dir, "poster.jpg"), PosterWidth); err == nil {
			hasP = true
		} else {
			sc.log.Warn("poster", "src", poster, "err", err)
		}
	}
	if backdrop != "" {
		if err := resizeImage(ctx, backdrop, filepath.Join(dir, "backdrop.jpg"), BackdropWidth); err == nil {
			hasB = true
		} else {
			sc.log.Warn("backdrop", "src", backdrop, "err", err)
		}
	}
	// No local file: on a TMDB-matched item the artwork already there came from TMDB.
	// Keep it, or a re-scan (a new subtitle, a schema migration) wipes it for good, since
	// the metadata refresh does not visit matched items again.
	if (!hasP || !hasB) && sc.st.HasOnlineMatch(id) {
		oldP, oldB := sc.st.ImageFlags(id)
		hasP = hasP || (poster == "" && oldP)
		hasB = hasB || (backdrop == "" && oldB)
	}
	if !hasP {
		os.Remove(filepath.Join(dir, "poster.jpg"))
	}
	if !hasB {
		os.Remove(filepath.Join(dir, "backdrop.jpg"))
	}
	sc.st.SetImageFlags(id, hasP, hasB)
}

func (sc *Scanner) processFile(ctx context.Context, lib *store.Library, f *fileEntry, containers map[string]int64) error {
	pr, err := media.Probe(ctx, f.abs)
	if err != nil {
		return err
	}
	rec := &store.FileRecord{
		LibraryID: lib.ID, Kind: f.parsed.Kind, Key: "file:" + f.rel,
		Path: f.abs, Size: f.size, Mtime: f.mtime, Sig: f.sig,
		Container: pr.Container, VideoCodec: pr.VideoCodec, AudioCodec: pr.AudioCodec,
		Width: pr.Width, Height: pr.Height, Bitrate: pr.Bitrate, HDR: pr.HDR,
		RuntimeSec: int(pr.Duration + 0.5), CreditsStart: pr.CreditsStart,
	}
	if f.known != nil {
		rec.ID = f.known.ID
	}
	p := f.parsed
	if p.Kind == "episode" {
		sid := containers[p.SeriesKey]
		season := containers[fmt.Sprintf("%s/s%d", p.SeriesKey, p.Season)]
		rec.SeriesID, rec.ParentID = &sid, &season
		s := p.Season
		rec.SeasonNum = &s
		if p.Episode > 0 {
			e := p.Episode
			rec.EpisodeNum = &e
		}
		if p.EpisodeEnd > 0 {
			e := p.EpisodeEnd
			rec.EpisodeEnd = &e
		}
		rec.Title = p.Title
		if rec.Title == "" {
			if p.Episode > 0 {
				rec.Title = fmt.Sprintf("%d. rész", p.Episode)
			} else {
				rec.Title = f.base
			}
		}
	} else {
		rec.Title, rec.Year = p.Title, p.Year
	}

	for _, s := range pr.Streams {
		rec.Streams = append(rec.Streams, store.StreamRow{Idx: s.Index, Type: s.Type, TypeIdx: s.TypeIndex, Codec: s.Codec,
			Lang: langOr(s.Lang), Title: s.Title, Channels: s.Channels, Default: s.Default, Forced: s.Forced, HearingImpaired: s.HearingImpaired})
	}
	nsub := 0
	for _, s := range pr.Streams {
		if s.Type == "subtitle" {
			nsub++
		}
	}
	for _, ex := range externalSubs(f) {
		ex.TypeIdx = nsub
		ex.Idx = -1 - nsub
		nsub++
		rec.Streams = append(rec.Streams, ex)
	}
	for _, c := range pr.Chapters {
		rec.Chapters = append(rec.Chapters, store.ChapterRow{Start: c.Start, Title: c.Title})
	}

	// local NFO: <video>.nfo, or movie.nfo for a movie alone in its folder
	if real, ok := f.listing[strings.ToLower(f.base)+".nfo"]; ok {
		rec.Meta = ReadNFO(filepath.Join(f.dir, real))
	} else if real, ok := f.listing["movie.nfo"]; ok && f.solo && p.Kind == "movie" {
		rec.Meta = ReadNFO(filepath.Join(f.dir, real))
	}

	id, err := sc.st.SaveFile(rec, SortTitle)
	if err != nil {
		return fmt.Errorf("mentés: %w", err)
	}
	if p.Kind == "movie" {
		imdb := p.ImdbID
		if rec.Meta != nil && rec.Meta.ImdbID != "" {
			imdb = rec.Meta.ImdbID
		}
		if err := sc.st.SetImdbID(id, imdb); err != nil {
			sc.log.Warn("imdb id", "path", f.abs, "err", err)
		}
	}

	// artwork
	poster, backdrop, thumb := "", "", ""
	poster = f.listing.find(f.dir, f.base+"-poster")
	backdrop = f.listing.find(f.dir, f.base+"-fanart", f.base+"-backdrop")
	thumb = f.listing.find(f.dir, f.base+"-thumb", f.base)
	if p.Kind == "movie" && f.solo {
		if poster == "" {
			poster = f.listing.find(f.dir, posterNames...)
		}
		if backdrop == "" {
			backdrop = f.listing.find(f.dir, backdropNames...)
		}
	}
	sc.storeArtwork(ctx, id, poster, backdrop)
	tdst := filepath.Join(ImageDir(sc.imgDir, id), "thumb.jpg")
	if thumb != "" {
		err = resizeImage(ctx, thumb, tdst, ThumbWidth)
	} else {
		err = videoThumb(ctx, f.abs, tdst, pr.Duration*0.2, ThumbWidth)
	}
	if err != nil {
		sc.log.Warn("thumbnail", "path", f.abs, "err", err)
	}
	return nil
}

func langOr(l string) string {
	if n := NormalizeLang(l); n != "" {
		return n
	}
	return strings.ToLower(l)
}

// externalSubs finds "<video>.<lang>[.forced].srt|vtt|ass|ssa" next to the video.
func externalSubs(f *fileEntry) []store.StreamRow {
	var out []store.StreamRow
	lb := strings.ToLower(f.base)
	var names []string
	for low := range f.listing {
		if subtitleExts[filepath.Ext(low)] && (strings.HasPrefix(low, lb+".") || low == lb+filepath.Ext(low)) {
			names = append(names, low)
		}
	}
	sort.Strings(names)
	for _, low := range names {
		ext := filepath.Ext(low)
		middle := strings.TrimSuffix(strings.TrimPrefix(low, lb), ext)
		st := store.StreamRow{Type: "subtitle", Codec: map[string]string{".srt": "subrip", ".vtt": "webvtt", ".ass": "ass", ".ssa": "ssa"}[ext],
			External: filepath.Join(f.dir, f.listing[low])}
		for _, part := range strings.Split(strings.Trim(middle, "."), ".") {
			switch part {
			case "forced", "foreign":
				st.Forced = true
			case "default":
				st.Default = true
			case "sdh", "cc", "hi", "":
			default:
				if l := NormalizeLang(part); l != "" && st.Lang == "" {
					st.Lang = l
				}
			}
		}
		st.Title = "Külső"
		out = append(out, st)
	}
	return out
}
