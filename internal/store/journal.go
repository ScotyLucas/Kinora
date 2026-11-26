package store

import (
	"database/sql"
	"strings"
)

// The Napló: every finished viewing of a film or episode, kept as a snapshot (title, runtime,
// genres) so the statistics stay right after the file, the item or the library is gone.

// eventDedupSec: a player keeps reporting progress past the played mark, and a title marked
// played twice in a row is one viewing; within this window a second finish is not a new event.
const eventDedupSec = 6 * 3600

// unmarkWindowSec: marking a title unwatched takes back a viewing recorded this recently
// (a mis-tap), never an old one: last year's viewing stays in the Napló.
const unmarkWindowSec = 24 * 3600

// recordEvent appends a finished viewing. source: play, mark or import.
func recordEvent(q execer, profileID, itemID, at int64, source string) {
	q.Exec(`INSERT INTO watch_events(profile_id,item_id,series_id,kind,title,series_title,season,episode,year,runtime_sec,genres,source,watched_at,tmdb_id)
		SELECT ?, i.id, s.id, i.kind, i.title, COALESCE(s.title,''), i.season_num, i.episode_num, COALESCE(i.year,s.year), i.runtime_sec,
			COALESCE((SELECT group_concat(genre,'|') FROM item_genres WHERE item_id=COALESCE(s.id,i.id)),''), ?, ?,
			MAX(0, CASE WHEN i.kind='episode' THEN COALESCE(s.tmdb_id,0) ELSE COALESCE(i.tmdb_id,0) END)
		FROM items i LEFT JOIN items s ON s.id=i.series_id AND i.kind='episode'
		WHERE i.id=? AND i.kind IN ('movie','episode')
		AND NOT EXISTS(SELECT 1 FROM watch_events WHERE profile_id=? AND item_id=i.id AND ABS(watched_at-?)<?)`,
		profileID, source, at, itemID, profileID, at, eventDedupSec)
}

// unrecordEvent takes back the latest viewing of an item if it was recorded within unmarkWindowSec.
func unrecordEvent(q execer, profileID, itemID int64) {
	q.Exec(`DELETE FROM watch_events WHERE id=(SELECT id FROM watch_events WHERE profile_id=? AND item_id=? AND watched_at>?
		ORDER BY watched_at DESC LIMIT 1)`, profileID, itemID, now()-unmarkWindowSec)
}

// JournalEvent is one viewing in the Napló.
type JournalEvent struct {
	ID          int64    `json:"id"`
	At          int64    `json:"at"`
	Kind        string   `json:"kind"`
	Title       string   `json:"title"`
	SeriesTitle string   `json:"series_title,omitempty"`
	Season      *int     `json:"season,omitempty"`
	Episode     *int     `json:"episode,omitempty"`
	Year        *int     `json:"year,omitempty"`
	RuntimeSec  int      `json:"runtime_sec"`
	Genres      []string `json:"genres"`
	Source      string   `json:"source"`
	// the library item to link to and show artwork of (the series for an episode); 0 when it is gone
	ItemID    int64 `json:"item_id,omitempty"`
	ImageID   int64 `json:"image_id,omitempty"`
	UpdatedAt int64 `json:"updated_at,omitempty"`
	// the film's TMDB id, or the series' for an episode (0 = not matched)
	TmdbID int64 `json:"tmdb_id,omitempty"`
	// the profile's 1..5 stars for this viewing (0 = not rated)
	Rating int `json:"rating"`
	// a TMDB poster path (/abc.jpg) for a viewing added by hand, whose title is not in the library
	Poster string `json:"poster,omitempty"`
	// from TMDB, once fetched: up to 2 directors (creators for a series), the top 5 of the cast, production countries (ISO 3166-1)
	Directors []string `json:"directors"`
	Cast      []string `json:"cast"`
	Countries []string `json:"countries"`
}

// JournalMax caps one answer; a year of daily viewing is well below it.
const JournalMax = 20000

// Journal returns a profile's viewings in [from, to), oldest first.
func (s *Store) Journal(profileID, from, to int64) ([]JournalEvent, error) {
	rows, err := s.DB.Query(`SELECT e.id, e.watched_at, e.kind, e.title, e.series_title, e.season, e.episode, e.year, e.runtime_sec, e.genres, e.source,
			COALESCE(i.id,0), COALESCE(CASE WHEN e.kind='episode' THEN s.id END, i.id, 0),
			COALESCE(CASE WHEN e.kind='episode' THEN s.updated_at END, i.updated_at, 0),
			e.tmdb_id, e.rating, e.poster, COALESCE(c.directors,''), COALESCE(c.cast_names,''), COALESCE(c.countries,'')
		FROM watch_events e LEFT JOIN items i ON i.id=e.item_id LEFT JOIN items s ON s.id=e.series_id
		LEFT JOIN tmdb_credits c ON e.tmdb_id>0 AND c.tmdb_id=e.tmdb_id AND c.kind=CASE WHEN e.kind='episode' THEN 'series' ELSE 'movie' END
		WHERE e.profile_id=? AND e.watched_at>=? AND e.watched_at<?
		ORDER BY e.watched_at LIMIT ?`, profileID, from, to, JournalMax)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JournalEvent{}
	for rows.Next() {
		var e JournalEvent
		var season, episode, year sql.NullInt64
		var genres, dirs, cast, countries string
		if err := rows.Scan(&e.ID, &e.At, &e.Kind, &e.Title, &e.SeriesTitle, &season, &episode, &year, &e.RuntimeSec, &genres, &e.Source,
			&e.ItemID, &e.ImageID, &e.UpdatedAt, &e.TmdbID, &e.Rating, &e.Poster, &dirs, &cast, &countries); err != nil {
			return nil, err
		}
		e.Season, e.Episode, e.Year = intPtr(season), intPtr(episode), intPtr(year)
		e.Genres, e.Directors, e.Cast, e.Countries = splitList(genres), splitList(dirs), splitList(cast), splitList(countries)
		out = append(out, e)
	}
	return out, rows.Err()
}

// splitList splits a "|"-joined snapshot list; empty gives [], not null.
func splitList(v string) []string {
	if v == "" {
		return []string{}
	}
	return strings.Split(v, "|")
}

func intPtr(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}

// JournalCount returns how many viewings a profile has in [from, to).
func (s *Store) JournalCount(profileID, from, to int64) (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM watch_events WHERE profile_id=? AND watched_at>=? AND watched_at<?`, profileID, from, to).Scan(&n)
	return n, err
}

// SetJournalRecapSeen records that the profile closed (or opened) the recap card of a month (YYYY-MM).
func (s *Store) SetJournalRecapSeen(profileID int64, month string) error {
	_, err := s.DB.Exec(`UPDATE profiles SET journal_recap_seen=? WHERE id=?`, month, profileID)
	return err
}
