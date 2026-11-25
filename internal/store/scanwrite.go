package store

import (
	"database/sql"
	"strings"
)

// KnownFile is the scanner's view of an existing file item.
type KnownFile struct {
	ID      int64
	Path    string
	Size    int64
	Mtime   int64
	Missing bool
	Sig     string
	Kind    string // movie | episode: a file parsed differently now is processed again
	// SeriesID / ParentID: an episode whose series or season changed (a better parse merged
	// its folder into another show) is processed again.
	SeriesID, ParentID int64
}

// KnownFiles returns path → KnownFile for every file-backed item of a library.
func (s *Store) KnownFiles(libID int64) (map[string]*KnownFile, error) {
	rows, err := s.DB.Query(`SELECT id,path,size,mtime,missing,sidecar_sig,kind,COALESCE(series_id,0),COALESCE(parent_id,0) FROM items WHERE library_id=? AND kind IN ('movie','episode')`, libID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*KnownFile{}
	for rows.Next() {
		k := &KnownFile{}
		if err := rows.Scan(&k.ID, &k.Path, &k.Size, &k.Mtime, &k.Missing, &k.Sig, &k.Kind, &k.SeriesID, &k.ParentID); err != nil {
			return nil, err
		}
		out[k.Path] = k
	}
	return out, rows.Err()
}

// KnownContainer is an existing series/season item.
type KnownContainer struct {
	ID  int64
	Sig string
}

// KnownContainers returns key → series/season items of a library.
func (s *Store) KnownContainers(libID int64) (map[string]*KnownContainer, error) {
	rows, err := s.DB.Query(`SELECT id,key,sidecar_sig FROM items WHERE library_id=? AND kind IN ('series','season')`, libID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*KnownContainer{}
	for rows.Next() {
		var key string
		k := &KnownContainer{}
		if err := rows.Scan(&k.ID, &key, &k.Sig); err != nil {
			return nil, err
		}
		out[key] = k
	}
	return out, rows.Err()
}

// LocalMeta is metadata read from a local NFO file.
type LocalMeta struct {
	Title         string
	Year          int
	Overview      string
	ContentRating string
	Rating        *float64
	Genres        []string
	Tags          []string
	TmdbID        int64
	ImdbID        string // read by the scanner, stored through SetImdbID
}

// StreamRow is a stream to persist.
type StreamRow struct {
	Idx             int
	Type            string
	TypeIdx         int
	Codec           string
	Lang            string
	Title           string
	Channels        int
	Default         bool
	Forced          bool
	HearingImpaired bool
	External        string
}

// ChapterRow is a chapter marker to persist.
type ChapterRow struct {
	Start float64
	Title string
}

// FileRecord is everything the scanner learned about one video file.
type FileRecord struct {
	ID         int64 // existing id (update / move), 0 = insert
	LibraryID  int64
	Kind       string // movie | episode
	ParentID   *int64
	SeriesID   *int64
	Key        string
	Title      string
	Year       int
	SeasonNum  *int
	EpisodeNum *int
	EpisodeEnd *int
	Path       string
	Size       int64
	Mtime      int64
	Sig        string

	Container    string
	VideoCodec   string
	AudioCodec   string
	Width        int
	Height       int
	Bitrate      int
	HDR          string
	RuntimeSec   int
	CreditsStart *float64
	Streams      []StreamRow
	Chapters     []ChapterRow
	Meta         *LocalMeta
}

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

// SaveFile inserts or updates a file item with its streams, chapters and local metadata.
// Online/manual metadata (tmdb_id not NULL/0) is never overwritten by filename parsing.
func (s *Store) SaveFile(r *FileRecord, sortTitle func(string) string) (int64, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	t := now()
	id := r.ID
	if id == 0 {
		// The same key may exist (e.g. re-added after a library path change).
		tx.QueryRow(`SELECT id FROM items WHERE library_id=? AND key=?`, r.LibraryID, r.Key).Scan(&id)
	}
	if id == 0 {
		res, err := tx.Exec(`INSERT INTO items(library_id,kind,key,title,sort_title,added_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
			r.LibraryID, r.Kind, r.Key, r.Title, sortTitle(r.Title), t, t)
		if err != nil {
			return 0, err
		}
		id, _ = res.LastInsertId()
	}
	var tmdb sql.NullInt64
	tx.QueryRow(`SELECT tmdb_id FROM items WHERE id=?`, id).Scan(&tmdb)
	parsedOwnsTitle := !tmdb.Valid || tmdb.Int64 == 0

	_, err = tx.Exec(`UPDATE items SET kind=?,key=?,parent_id=?,series_id=?,season_num=?,episode_num=?,episode_end=?,
		path=?,size=?,mtime=?,sidecar_sig=?,container=?,video_codec=?,audio_codec=?,width=?,height=?,bitrate=?,hdr=?,
		runtime_sec=?,credits_start=?,missing=0,updated_at=? WHERE id=?`,
		r.Kind, r.Key, r.ParentID, r.SeriesID, r.SeasonNum, r.EpisodeNum, r.EpisodeEnd,
		r.Path, r.Size, r.Mtime, r.Sig, r.Container, r.VideoCodec, r.AudioCodec, r.Width, r.Height, r.Bitrate, r.HDR,
		r.RuntimeSec, r.CreditsStart, t, id)
	if err != nil {
		return 0, err
	}
	if parsedOwnsTitle {
		if _, err := tx.Exec(`UPDATE items SET title=?,sort_title=?,year=? WHERE id=?`, r.Title, sortTitle(r.Title), nullInt(r.Year), id); err != nil {
			return 0, err
		}
	}
	if r.Meta != nil {
		if err := applyLocalMeta(tx, id, r.Meta, sortTitle); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(`DELETE FROM media_streams WHERE item_id=?`, id); err != nil {
		return 0, err
	}
	for _, st := range r.Streams {
		if _, err := tx.Exec(`INSERT OR REPLACE INTO media_streams(item_id,idx,type,type_idx,codec,lang,title,channels,is_default,is_forced,hearing_impaired,external_path)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, id, st.Idx, st.Type, st.TypeIdx, st.Codec, st.Lang, st.Title, st.Channels, st.Default, st.Forced, st.HearingImpaired, st.External); err != nil {
			return 0, err
		}
	}
	if _, err := tx.Exec(`DELETE FROM chapters WHERE item_id=?`, id); err != nil {
		return 0, err
	}
	for _, c := range r.Chapters {
		if _, err := tx.Exec(`INSERT INTO chapters(item_id,start,title) VALUES(?,?,?)`, id, c.Start, c.Title); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

func applyLocalMeta(tx *sql.Tx, id int64, m *LocalMeta, sortTitle func(string) string) error {
	set := []string{"tmdb_id=?"}
	tmdb := m.TmdbID
	if tmdb == 0 {
		tmdb = -1 // local metadata: the online fetcher must not overwrite it
	}
	args := []any{tmdb}
	if m.Title != "" {
		set = append(set, "title=?", "sort_title=?")
		args = append(args, m.Title, sortTitle(m.Title))
	}
	if m.Year > 0 {
		set = append(set, "year=?")
		args = append(args, m.Year)
	}
	if m.Overview != "" {
		set = append(set, "overview=?")
		args = append(args, m.Overview)
	}
	if m.ContentRating != "" {
		set = append(set, "content_rating=?", "rating_level=?")
		lvl, ok := RatingLevel(m.ContentRating)
		var lv any
		if ok {
			lv = lvl
		}
		args = append(args, m.ContentRating, lv)
	}
	if m.Rating != nil {
		set = append(set, "community_rating=?")
		args = append(args, *m.Rating)
	}
	args = append(args, id)
	if _, err := tx.Exec(`UPDATE items SET `+strings.Join(set, ",")+` WHERE id=?`, args...); err != nil {
		return err
	}
	if len(m.Genres) > 0 {
		if _, err := tx.Exec(`DELETE FROM item_genres WHERE item_id=?`, id); err != nil {
			return err
		}
		for _, g := range m.Genres {
			tx.Exec(`INSERT OR IGNORE INTO item_genres(item_id,genre) VALUES(?,?)`, id, g)
		}
	}
	for _, tg := range m.Tags {
		tx.Exec(`INSERT OR IGNORE INTO item_tags(item_id,tag) VALUES(?,?)`, id, tg)
	}
	return nil
}

// ContainerRecord describes a series or season folder.
type ContainerRecord struct {
	LibraryID int64
	Kind      string // series | season
	Key       string
	Title     string
	Year      int
	ParentID  *int64
	SeriesID  *int64
	SeasonNum *int
	Path      string // folder (informational)
	Sig       string
	Meta      *LocalMeta
}

// UpsertContainer inserts or updates a series/season item and returns its id.
func (s *Store) UpsertContainer(r *ContainerRecord, sortTitle func(string) string) (int64, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	t := now()
	var id int64
	var tmdb sql.NullInt64
	err = tx.QueryRow(`SELECT id,tmdb_id FROM items WHERE library_id=? AND key=?`, r.LibraryID, r.Key).Scan(&id, &tmdb)
	if err == sql.ErrNoRows {
		res, err := tx.Exec(`INSERT INTO items(library_id,kind,key,title,sort_title,year,parent_id,series_id,season_num,path,sidecar_sig,added_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, r.LibraryID, r.Kind, r.Key, r.Title, sortTitle(r.Title), nullInt(r.Year),
			r.ParentID, r.SeriesID, r.SeasonNum, r.Path, r.Sig, t, t)
		if err != nil {
			return 0, err
		}
		id, _ = res.LastInsertId()
		if r.Kind == "series" {
			tx.Exec(`UPDATE items SET series_id=? WHERE id=?`, id, id)
		}
	} else if err != nil {
		return 0, err
	} else {
		if _, err := tx.Exec(`UPDATE items SET parent_id=?,series_id=COALESCE(?,series_id),season_num=?,path=?,sidecar_sig=?,updated_at=? WHERE id=?`,
			r.ParentID, r.SeriesID, r.SeasonNum, r.Path, r.Sig, t, id); err != nil {
			return 0, err
		}
		if !tmdb.Valid || tmdb.Int64 == 0 {
			tx.Exec(`UPDATE items SET title=?,sort_title=?,year=? WHERE id=?`, r.Title, sortTitle(r.Title), nullInt(r.Year), id)
		}
	}
	if r.Meta != nil {
		if err := applyLocalMeta(tx, id, r.Meta, sortTitle); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// MarkMissing flags items whose file vanished (never deletes: user data survives
// temporary outages of network shares).
func (s *Store) MarkMissing(ids []int64) error {
	for _, id := range ids {
		if _, err := s.DB.Exec(`UPDATE items SET missing=1, updated_at=? WHERE id=? AND missing=0`, now(), id); err != nil {
			return err
		}
	}
	return nil
}

// FinishScan recomputes derived container state of a library:
// seasons/series without present episodes become missing (and come back
// automatically), and episodes/seasons inherit the series' content rating.
func (s *Store) FinishScan(libID int64) error {
	stmts := []string{
		`UPDATE items SET missing = CASE WHEN EXISTS(SELECT 1 FROM items e WHERE e.parent_id=items.id AND e.kind='episode' AND e.missing=0) THEN 0 ELSE 1 END
		 WHERE library_id=? AND kind='season'`,
		`UPDATE items SET missing = CASE WHEN EXISTS(SELECT 1 FROM items e WHERE e.series_id=items.id AND e.kind='episode' AND e.missing=0) THEN 0 ELSE 1 END
		 WHERE library_id=? AND kind='series'`,
		`UPDATE items SET
		   rating_level=(SELECT sr.rating_level FROM items sr WHERE sr.id=items.series_id),
		   content_rating=(SELECT sr.content_rating FROM items sr WHERE sr.id=items.series_id)
		 WHERE library_id=? AND kind IN ('season','episode') AND series_id IS NOT NULL`,
		// series runtime/year helpers for sorting & filters
		`UPDATE items SET added_at=COALESCE((SELECT MAX(e.added_at) FROM items e WHERE e.series_id=items.id AND e.kind='episode' AND e.missing=0), added_at)
		 WHERE library_id=? AND kind='series'`,
	}
	for _, q := range stmts {
		if _, err := s.DB.Exec(q, libID); err != nil {
			return err
		}
	}
	return nil
}

// SetImageFlags records which artwork exists for an item.
func (s *Store) SetImageFlags(id int64, poster, backdrop bool) error {
	_, err := s.DB.Exec(`UPDATE items SET has_poster=?, has_backdrop=? WHERE id=?`, poster, backdrop, id)
	return err
}

// HasOnlineMatch reports whether an item, or the series it belongs to, is matched on TMDB:
// its artwork may then come from TMDB rather than from files next to the video.
func (s *Store) HasOnlineMatch(id int64) bool {
	var ok bool
	s.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM items i LEFT JOIN items sr ON sr.id=i.series_id
		WHERE i.id=? AND (i.tmdb_id>0 OR sr.tmdb_id>0))`, id).Scan(&ok)
	return ok
}

// ImageFlags returns the stored artwork flags.
func (s *Store) ImageFlags(id int64) (poster, backdrop bool) {
	s.DB.QueryRow(`SELECT has_poster,has_backdrop FROM items WHERE id=?`, id).Scan(&poster, &backdrop)
	return
}
