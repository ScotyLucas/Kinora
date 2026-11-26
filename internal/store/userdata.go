package store

import (
	"database/sql"
	"strings"
)

// UserData is per-profile state of an item.
type UserData struct {
	Position     float64 `json:"position"`
	Played       bool    `json:"played"`
	PlayCount    int     `json:"play_count"`
	LastPlayedAt *int64  `json:"last_played_at"`
	Favorite     bool    `json:"favorite"`
	Stars        *int    `json:"stars"`
	Hidden       bool    `json:"hidden"`
}

// GetUserData returns the profile's state for an item (zero value if none).
func (s *Store) GetUserData(profileID, itemID int64) UserData {
	var d UserData
	var last sql.NullInt64
	var stars sql.NullInt64
	s.DB.QueryRow(`SELECT position,played,play_count,last_played_at,favorite,stars,hidden FROM user_data WHERE profile_id=? AND item_id=?`,
		profileID, itemID).Scan(&d.Position, &d.Played, &d.PlayCount, &last, &d.Favorite, &stars, &d.Hidden)
	if last.Valid {
		d.LastPlayedAt = &last.Int64
	}
	if stars.Valid {
		v := int(stars.Int64)
		d.Stars = &v
	}
	return d
}

type execer interface {
	Exec(string, ...any) (sql.Result, error)
}

func ensureUD(q execer, profileID, itemID int64) error {
	_, err := q.Exec(`INSERT OR IGNORE INTO user_data(profile_id,item_id) VALUES(?,?)`, profileID, itemID)
	return err
}

// PlayedThreshold marks an item played after this fraction of its runtime.
const PlayedThreshold = 0.92

// EndedMinFraction is how far in a reported end has to be to count (the credits of a
// long film can start before 92%; a broken stream "ends" anywhere). The runtime is
// metadata and can be off, so this stays well below the credits.
const EndedMinFraction = 0.75

// ReportProgress stores a playback position. Past 92% the item becomes played.
// Any progress un-hides the item and its series (rewatching brings it back).
func (s *Store) ReportProgress(profileID int64, it *Item, position float64, ended bool) (played bool, err error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err := ensureUD(tx, profileID, it.ID); err != nil {
		return false, err
	}
	t := now()
	if position < 0 {
		position = 0
	}
	frac := 0.0
	if it.RuntimeSec > 0 {
		frac = position / float64(it.RuntimeSec)
	}
	// A player reports "ended" when its stream runs out, and a stream cut off by a failed
	// transcode runs out early: far from the end, that is not the end of the film.
	if ended && it.RuntimeSec > 0 && frac < EndedMinFraction {
		ended = false
	}
	done := ended || frac >= PlayedThreshold
	if done {
		_, err = tx.Exec(`UPDATE user_data SET position=0, played=1, play_count=play_count+CASE WHEN played=1 AND position=0 THEN 0 ELSE 1 END,
			last_played_at=?, hidden=0 WHERE profile_id=? AND item_id=?`, t, profileID, it.ID)
	} else {
		_, err = tx.Exec(`UPDATE user_data SET position=?, last_played_at=?, hidden=0 WHERE profile_id=? AND item_id=?`, position, t, profileID, it.ID)
	}
	if err != nil {
		return false, err
	}
	if done {
		recordWatched(tx, profileID, it.ID, t)
		recordEvent(tx, profileID, it.ID, t, "play")
	}
	if it.SeriesID != nil {
		tx.Exec(`UPDATE user_data SET hidden=0 WHERE profile_id=? AND item_id=?`, profileID, *it.SeriesID)
	}
	if it.Kind == "movie" && it.ImdbID != "" {
		if err := mirrorUserData(tx, profileID, it.ID); err != nil {
			return false, err
		}
		tx.Exec(`INSERT INTO version_prefs(profile_id,imdb_id,item_id,updated_at) VALUES(?,?,?,?)
			ON CONFLICT(profile_id,imdb_id) DO UPDATE SET item_id=excluded.item_id, updated_at=excluded.updated_at`, profileID, it.ImdbID, it.ID, t)
	}
	return done, tx.Commit()
}

// episodeIDs returns the present episodes below a series/season.
func episodeIDs(q interface {
	Query(string, ...any) (*sql.Rows, error)
}, it *Item) []int64 {
	var ids []int64
	var rows *sql.Rows
	var err error
	if it.Kind == "series" {
		rows, err = q.Query(`SELECT id FROM items WHERE series_id=? AND kind='episode' AND missing=0`, it.ID)
	} else {
		rows, err = q.Query(`SELECT id FROM items WHERE parent_id=? AND kind='episode' AND missing=0`, it.ID)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	return ids
}

// SetPlayed marks an item (or every episode of a series/season) played or unplayed.
func (s *Store) SetPlayed(profileID int64, it *Item, played bool) error {
	ids := []int64{it.ID}
	if it.Kind == "series" || it.Kind == "season" {
		ids = episodeIDs(s.DB, it)
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t := now()
	for _, id := range ids {
		ensureUD(tx, profileID, id)
		if played {
			_, err = tx.Exec(`UPDATE user_data SET played=1, position=0, play_count=MAX(play_count,1), last_played_at=? WHERE profile_id=? AND item_id=?`, t, profileID, id)
		} else {
			_, err = tx.Exec(`UPDATE user_data SET played=0, position=0 WHERE profile_id=? AND item_id=?`, profileID, id)
		}
		if err != nil {
			return err
		}
		if played {
			recordWatched(tx, profileID, id, t)
			recordEvent(tx, profileID, id, t, "mark")
		} else {
			forgetWatched(tx, profileID, id)
			unrecordEvent(tx, profileID, id)
		}
	}
	if it.Kind == "movie" {
		if err := mirrorUserData(tx, profileID, it.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetFlag sets favorite or hidden.
func (s *Store) SetFlag(profileID, itemID int64, flag string, v bool) error {
	col := map[string]string{"favorite": "favorite", "hidden": "hidden"}[flag]
	if col == "" {
		return ErrNotFound
	}
	if err := ensureUD(s.DB, profileID, itemID); err != nil {
		return err
	}
	if _, err := s.DB.Exec(`UPDATE user_data SET `+col+`=? WHERE profile_id=? AND item_id=?`, v, profileID, itemID); err != nil {
		return err
	}
	return mirrorUserData(s.DB, profileID, itemID)
}

// SetStars sets a 1..5 rating (nil clears it).
func (s *Store) SetStars(profileID, itemID int64, stars *int) error {
	if err := ensureUD(s.DB, profileID, itemID); err != nil {
		return err
	}
	if _, err := s.DB.Exec(`UPDATE user_data SET stars=? WHERE profile_id=? AND item_id=?`, stars, profileID, itemID); err != nil {
		return err
	}
	return mirrorUserData(s.DB, profileID, itemID)
}

// SetTags replaces the tags of an item.
func (s *Store) SetTags(itemID int64, tags []string) ([]string, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM item_tags WHERE item_id=?`, itemID); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	out := []string{}
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" || len([]rune(t)) > 40 || seen[strings.ToLower(t)] {
			continue
		}
		seen[strings.ToLower(t)] = true
		out = append(out, t)
		if _, err := tx.Exec(`INSERT OR IGNORE INTO item_tags(item_id,tag) VALUES(?,?)`, itemID, t); err != nil {
			return nil, err
		}
	}
	return out, tx.Commit()
}
