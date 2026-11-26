package store

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
)

// Napló 2: star ratings, viewings added by hand, abandoned titles, goals, challenges and the
// TMDB credits cache behind the director, cast and country breakdowns.

// ErrNotManual: only a viewing added by hand can be deleted; a played one is taken back by unmarking.
var ErrNotManual = errors.New("csak kézzel felvett nézés törölhető")

// SetEventRating sets the 1..5 stars of one viewing (0 clears them). The title's own rating in
// the library follows, when the item is still there, so the detail page shows the same stars.
func (s *Store) SetEventRating(profileID, eventID int64, rating int) error {
	var itemID sql.NullInt64
	err := s.DB.QueryRow(`SELECT item_id FROM watch_events WHERE id=? AND profile_id=?`, eventID, profileID).Scan(&itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if _, err := s.DB.Exec(`UPDATE watch_events SET rating=? WHERE id=?`, rating, eventID); err != nil {
		return err
	}
	if itemID.Valid {
		var stars *int
		if rating > 0 {
			stars = &rating
		}
		return s.SetStars(profileID, itemID.Int64, stars)
	}
	return nil
}

// ManualEvent is a film seen outside Kinora (at the cinema, elsewhere), from TMDB.
type ManualEvent struct {
	TmdbID     int64
	Title      string
	Year       int
	RuntimeSec int
	Genres     []string
	Poster     string
	At         int64
	Rating     int
	// the library's copy of the film, when there is one (0 = not in the library)
	ItemID int64
}

// ErrDuplicate: the same film is already in the Napló within eventDedupSec of that time.
var ErrDuplicate = errors.New("ez a nézés már szerepel a naplóban")

// AddManualEvent records a viewing added by hand and returns its id.
func (s *Store) AddManualEvent(profileID int64, m ManualEvent) (int64, error) {
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM watch_events WHERE profile_id=? AND kind='movie' AND tmdb_id=? AND ABS(watched_at-?)<?`,
		profileID, m.TmdbID, m.At, eventDedupSec).Scan(&n)
	if n > 0 {
		return 0, ErrDuplicate
	}
	var item any
	if m.ItemID > 0 {
		item = m.ItemID
	}
	var year any
	if m.Year > 0 {
		year = m.Year
	}
	res, err := s.DB.Exec(`INSERT INTO watch_events(profile_id,item_id,kind,title,year,runtime_sec,genres,source,watched_at,tmdb_id,rating,poster)
		VALUES(?,?,'movie',?,?,?,?,'manual',?,?,?,?)`,
		profileID, item, m.Title, year, m.RuntimeSec, strings.Join(m.Genres, "|"), m.At, m.TmdbID, m.Rating, m.Poster)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeleteEvent removes a viewing added by hand.
func (s *Store) DeleteEvent(profileID, eventID int64) error {
	var src string
	err := s.DB.QueryRow(`SELECT source FROM watch_events WHERE id=? AND profile_id=?`, eventID, profileID).Scan(&src)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if src != "manual" {
		return ErrNotManual
	}
	_, err = s.DB.Exec(`DELETE FROM watch_events WHERE id=?`, eventID)
	return err
}

// LibraryMovieByTmdb returns the library film with that TMDB id the profile can see (0 = none).
func (s *Store) LibraryMovieByTmdb(p *Profile, tmdbID int64) int64 {
	items, err := s.QueryItems(p, ` AND i.kind='movie' AND i.missing=0 AND i.tmdb_id=?`, []any{tmdbID}, `LIMIT 1`)
	if err != nil || len(items) == 0 {
		return 0
	}
	return items[0].ID
}

// Abandoned titles: started, left before abandonedShare of the runtime, and not touched for
// abandonedAfterSec (until then they are simply in Continue Watching).
const (
	abandonedShare    = 0.2
	abandonedAfterSec = 14 * 86400
	abandonedMax      = 60
)

// Abandoned returns the films and episodes the profile started and left early, newest first;
// a series shows once, with the episode it was left at.
func (s *Store) Abandoned(p *Profile) ([]*Item, error) {
	items, err := s.QueryItems(p, ` AND i.kind IN ('movie','episode') AND i.missing=0 AND i.runtime_sec>0
		AND ud.position>0 AND COALESCE(ud.played,0)=0 AND ud.position < i.runtime_sec*?
		AND ud.last_played_at < ?
		AND NOT EXISTS(SELECT 1 FROM journal_hidden jh WHERE jh.profile_id=ud.profile_id AND jh.item_id IN (i.id, COALESCE(i.series_id,0)))`+NotAltVersion,
		[]any{abandonedShare, now() - abandonedAfterSec}, `ORDER BY ud.last_played_at DESC LIMIT ?`, abandonedMax*3)
	if err != nil {
		return nil, err
	}
	out, seen := []*Item{}, map[int64]bool{}
	for _, it := range items {
		if it.SeriesID != nil {
			if seen[*it.SeriesID] {
				continue
			}
			seen[*it.SeriesID] = true
		}
		out = append(out, it)
		if len(out) == abandonedMax {
			break
		}
	}
	return out, nil
}

// HideAbandoned puts a title (a film, or a whole series) away from the abandoned list.
func (s *Store) HideAbandoned(profileID, itemID int64) error {
	_, err := s.DB.Exec(`INSERT OR IGNORE INTO journal_hidden(profile_id,item_id) VALUES(?,?)`, profileID, itemID)
	return err
}

// JournalGoal is one goal beyond the yearly film goal kept on the profile.
type JournalGoal struct {
	ID     int64  `json:"id"`
	Period string `json:"period"` // year or month
	Metric string `json:"metric"` // films, hours, episodes or genre (films of that genre)
	Genre  string `json:"genre,omitempty"`
	Target int    `json:"target"`
}

// JournalGoalsMax caps the goals of a profile.
const JournalGoalsMax = 12

// ErrTooMany: the profile already has the most goals or challenges allowed.
var ErrTooMany = errors.New("elérted a legtöbbet, amennyi felvehető")

func (s *Store) JournalGoals(profileID int64) ([]JournalGoal, error) {
	rows, err := s.DB.Query(`SELECT id,period,metric,genre,target FROM journal_goals WHERE profile_id=? ORDER BY id`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JournalGoal{}
	for rows.Next() {
		var g JournalGoal
		if err := rows.Scan(&g.ID, &g.Period, &g.Metric, &g.Genre, &g.Target); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) AddJournalGoal(profileID int64, g JournalGoal) (int64, error) {
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM journal_goals WHERE profile_id=?`, profileID).Scan(&n)
	if n >= JournalGoalsMax {
		return 0, ErrTooMany
	}
	res, err := s.DB.Exec(`INSERT INTO journal_goals(profile_id,period,metric,genre,target,created_at) VALUES(?,?,?,?,?,?)`,
		profileID, g.Period, g.Metric, g.Genre, g.Target, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) DeleteJournalGoal(profileID, id int64) error {
	res, err := s.DB.Exec(`DELETE FROM journal_goals WHERE id=? AND profile_id=?`, id, profileID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Challenge is a list of titles to tick off ("every Nolan film").
type Challenge struct {
	ID      int64           `json:"id"`
	Title   string          `json:"title"`
	Items   []ChallengeItem `json:"items"`
	Done    int             `json:"done"`
	Total   int             `json:"total"`
	Created int64           `json:"created_at"`
}

// ChallengeItem is one title of a challenge; Watched says whether the profile has seen it (ever).
type ChallengeItem struct {
	Kind    string `json:"kind"` // movie or series
	TmdbID  int64  `json:"tmdb_id"`
	Title   string `json:"title"`
	Year    int    `json:"year,omitempty"`
	Poster  string `json:"poster,omitempty"`
	Watched bool   `json:"watched"`
	// the library item, when the title is in it
	ItemID int64 `json:"item_id,omitempty"`
}

const (
	ChallengesMax     = 30
	ChallengeItemsMax = 200
)

// Challenges returns the profile's challenges with what is ticked off. A film counts once it is in
// the Napló or the watch history; a series once any episode of it is.
func (s *Store) Challenges(p *Profile) ([]Challenge, error) {
	rows, err := s.DB.Query(`SELECT c.id, c.title, c.created_at, ci.kind, ci.tmdb_id, ci.title, COALESCE(ci.year,0), ci.poster,
			EXISTS(SELECT 1 FROM watch_events e WHERE e.profile_id=c.profile_id AND e.tmdb_id=ci.tmdb_id
				AND e.kind=CASE WHEN ci.kind='series' THEN 'episode' ELSE 'movie' END)
			OR EXISTS(SELECT 1 FROM watch_history w WHERE w.profile_id=c.profile_id AND w.tmdb_id=ci.tmdb_id AND w.kind=ci.kind)
		FROM journal_challenges c LEFT JOIN journal_challenge_items ci ON ci.challenge_id=c.id
		WHERE c.profile_id=? ORDER BY c.id, ci.year IS NULL, ci.year, ci.title`, p.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Challenge{}
	for rows.Next() {
		var id, created int64
		var title string
		var kind, ititle, poster sql.NullString
		var tmdb sql.NullInt64
		var year int
		var watched bool
		if err := rows.Scan(&id, &title, &created, &kind, &tmdb, &ititle, &year, &poster, &watched); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].ID != id {
			out = append(out, Challenge{ID: id, Title: title, Created: created, Items: []ChallengeItem{}})
		}
		if !tmdb.Valid {
			continue
		}
		c := &out[len(out)-1]
		c.Items = append(c.Items, ChallengeItem{Kind: kind.String, TmdbID: tmdb.Int64, Title: ititle.String, Year: year, Poster: poster.String, Watched: watched})
		c.Total++
		if watched {
			c.Done++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// link the titles that are in the library the profile can see
	lib := map[string]int64{}
	if items, err := s.QueryItems(p, ` AND i.kind IN ('movie','series') AND i.missing=0 AND i.tmdb_id>0
		AND i.tmdb_id IN (SELECT ci.tmdb_id FROM journal_challenge_items ci JOIN journal_challenges c ON c.id=ci.challenge_id WHERE c.profile_id=?)`,
		[]any{p.ID}, ""); err == nil {
		for _, it := range items {
			if it.TmdbID != nil {
				lib[credKey(it.Kind, *it.TmdbID)] = it.ID
			}
		}
	}
	for ci := range out {
		for ii := range out[ci].Items {
			x := &out[ci].Items[ii]
			x.ItemID = lib[credKey(x.Kind, x.TmdbID)]
		}
	}
	return out, nil
}

// AddChallenge creates a challenge with its titles (duplicates dropped, at most ChallengeItemsMax).
func (s *Store) AddChallenge(profileID int64, title string, items []ChallengeItem) (int64, error) {
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM journal_challenges WHERE profile_id=?`, profileID).Scan(&n)
	if n >= ChallengesMax {
		return 0, ErrTooMany
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO journal_challenges(profile_id,title,created_at) VALUES(?,?,?)`, profileID, title, now())
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if err := addChallengeItems(tx, id, items); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func addChallengeItems(tx *sql.Tx, id int64, items []ChallengeItem) error {
	var n int
	tx.QueryRow(`SELECT COUNT(*) FROM journal_challenge_items WHERE challenge_id=?`, id).Scan(&n)
	for _, it := range items {
		if n >= ChallengeItemsMax {
			break
		}
		var year any
		if it.Year > 0 {
			year = it.Year
		}
		res, err := tx.Exec(`INSERT OR IGNORE INTO journal_challenge_items(challenge_id,kind,tmdb_id,title,year,poster) VALUES(?,?,?,?,?,?)`,
			id, it.Kind, it.TmdbID, it.Title, year, it.Poster)
		if err != nil {
			return err
		}
		if k, _ := res.RowsAffected(); k > 0 {
			n++
		}
	}
	return nil
}

// ownChallenge checks that the challenge is the profile's.
func ownChallenge(q interface {
	QueryRow(string, ...any) *sql.Row
}, profileID, id int64) error {
	var x int
	if err := q.QueryRow(`SELECT 1 FROM journal_challenges WHERE id=? AND profile_id=?`, id, profileID).Scan(&x); err != nil {
		return ErrNotFound
	}
	return nil
}

// AddChallengeItems adds titles to a challenge of the profile.
func (s *Store) AddChallengeItems(profileID, id int64, items []ChallengeItem) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := ownChallenge(tx, profileID, id); err != nil {
		return err
	}
	if err := addChallengeItems(tx, id, items); err != nil {
		return err
	}
	return tx.Commit()
}

// RemoveChallengeItem drops one title of a challenge of the profile.
func (s *Store) RemoveChallengeItem(profileID, id int64, kind string, tmdbID int64) error {
	if err := ownChallenge(s.DB, profileID, id); err != nil {
		return err
	}
	_, err := s.DB.Exec(`DELETE FROM journal_challenge_items WHERE challenge_id=? AND kind=? AND tmdb_id=?`, id, kind, tmdbID)
	return err
}

// RenameChallenge sets the title of a challenge of the profile.
func (s *Store) RenameChallenge(profileID, id int64, title string) error {
	res, err := s.DB.Exec(`UPDATE journal_challenges SET title=? WHERE id=? AND profile_id=?`, title, id, profileID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteChallenge(profileID, id int64) error {
	res, err := s.DB.Exec(`DELETE FROM journal_challenges WHERE id=? AND profile_id=?`, id, profileID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CreditKey names a title whose TMDB credits are wanted.
type CreditKey struct {
	Kind   string // movie or series
	TmdbID int64
}

// Credits are what the breakdowns need of a title.
type Credits struct {
	Directors []string
	Cast      []string
	Countries []string
}

// MissingCredits lists the watched titles with no cached credits yet, most recently watched first.
func (s *Store) MissingCredits(limit int) ([]CreditKey, error) {
	rows, err := s.DB.Query(`SELECT CASE WHEN e.kind='episode' THEN 'series' ELSE 'movie' END AS k, e.tmdb_id, MAX(e.watched_at) AS last
		FROM watch_events e
		WHERE e.tmdb_id>0 AND NOT EXISTS(SELECT 1 FROM tmdb_credits c WHERE c.tmdb_id=e.tmdb_id AND c.kind=CASE WHEN e.kind='episode' THEN 'series' ELSE 'movie' END)
		GROUP BY k, e.tmdb_id ORDER BY last DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CreditKey{}
	for rows.Next() {
		var k CreditKey
		var last int64
		if err := rows.Scan(&k.Kind, &k.TmdbID, &last); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// clean drops the separator from names so a list splits back the same way.
func clean(list []string) string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		if v = strings.TrimSpace(strings.ReplaceAll(v, "|", "/")); v != "" {
			out = append(out, v)
		}
	}
	return strings.Join(out, "|")
}

// PutCredits caches the credits of a title (an empty set too, so it is not asked for again).
func (s *Store) PutCredits(k CreditKey, c Credits) error {
	_, err := s.DB.Exec(`INSERT OR REPLACE INTO tmdb_credits(kind,tmdb_id,directors,cast_names,countries,fetched_at) VALUES(?,?,?,?,?,?)`,
		k.Kind, k.TmdbID, clean(c.Directors), clean(c.Cast), clean(c.Countries), now())
	return err
}

func credKey(kind string, id int64) string { return kind + "|" + strconv.FormatInt(id, 10) }
