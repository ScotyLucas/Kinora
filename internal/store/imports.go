package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ------------------------------------------------------------------ imported accounts

// noPassword is stored for accounts created by an import: no password matches it (CheckPassword needs
// the "pbkdf2-sha256$…" format), so the account is usable only after its password link was used.
const noPassword = "!"

// ImportedUser returns the account an earlier import made of an external user (source "jellyfin", …).
func (s *Store) ImportedUser(source, externalID string) (*User, error) {
	var id int64
	err := s.DB.QueryRow(`SELECT user_id FROM import_links WHERE source=? AND external_id=?`, source, externalID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	return s.UserByID(id)
}

// UserByName finds an account by user name (case-insensitive).
func (s *Store) UserByName(name string) (*User, error) {
	u, err := scanUser(s.DB.QueryRow(`SELECT `+userCols+` FROM users WHERE username=? COLLATE NOCASE`, strings.TrimSpace(name)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// LinkImportedUser remembers that an external user is this account, so a later import finds it again
// (even after a rename on either side).
func (s *Store) LinkImportedUser(source, externalID string, userID int64) error {
	_, err := s.DB.Exec(`INSERT INTO import_links(source,external_id,user_id,created_at) VALUES(?,?,?,?)
		ON CONFLICT(source,external_id) DO UPDATE SET user_id=excluded.user_id`, source, externalID, userID, now())
	return err
}

// CreateUserWithoutPassword makes an account (with its first profile) nobody can sign in to until a
// password link sets its password.
func (s *Store) CreateUserWithoutPassword(username string) (*User, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	u, err := createUserTx(tx, username, "", false)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE users SET pass_hash=? WHERE id=?`, noPassword, u.ID); err != nil {
		return nil, err
	}
	return u, tx.Commit()
}

// HasPassword reports whether the account can sign in with a password.
func (u *User) HasPassword() bool { return u.passHash != noPassword && u.passHash != "" }

// ------------------------------------------------------------------ password links

// PasswordLinkTTL is how long a password link works.
const PasswordLinkTTL = 14 * 24 * time.Hour

// ErrLinkInvalid is returned for unknown, used or expired password links.
var ErrLinkInvalid = errors.New("a jelszóbeállító link érvénytelen vagy lejárt")

// CreatePasswordLink makes a one-time link that sets the account's password. Earlier links of the
// account stop working. Only a hash of the code is stored.
func (s *Store) CreatePasswordLink(userID int64) (code string, expires int64, err error) {
	code = RandomToken(16)
	expires = time.Now().Add(PasswordLinkTTL).Unix()
	tx, err := s.DB.Begin()
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM password_links WHERE user_id=?`, userID); err != nil {
		return "", 0, err
	}
	if _, err := tx.Exec(`INSERT INTO password_links(code_hash,user_id,expires_at,created_at) VALUES(?,?,?,?)`,
		hashToken(code), userID, expires, now()); err != nil {
		return "", 0, err
	}
	return code, expires, tx.Commit()
}

// PasswordLinkUser returns the account of a valid link.
func (s *Store) PasswordLinkUser(code string) (*User, error) {
	var id int64
	err := s.DB.QueryRow(`SELECT user_id FROM password_links WHERE code_hash=? AND expires_at>?`, hashToken(strings.TrimSpace(code)), now()).Scan(&id)
	if err != nil {
		return nil, ErrLinkInvalid
	}
	return s.UserByID(id)
}

// UsePasswordLink sets the password and consumes the link (atomically: a link works once).
func (s *Store) UsePasswordLink(code, password string) (*User, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id int64
	h := hashToken(strings.TrimSpace(code))
	if err := tx.QueryRow(`SELECT user_id FROM password_links WHERE code_hash=? AND expires_at>?`, h, now()).Scan(&id); err != nil {
		return nil, ErrLinkInvalid
	}
	if _, err := tx.Exec(`DELETE FROM password_links WHERE user_id=?`, id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE users SET pass_hash=? WHERE id=?`, HashPassword(password), id); err != nil {
		return nil, err
	}
	// sessions started with the old password (if any) end
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id=?`, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.UserByID(id)
}

// ------------------------------------------------------------------ imported watch state

// ImportedUserData is watch state from another server.
type ImportedUserData struct {
	Played     bool
	PlayCount  int
	Position   float64 // seconds
	Favorite   bool
	LastPlayed int64 // unix seconds, 0 = unknown
}

// MergeUserData merges imported state into a profile's state without ever losing what Kinora knows:
// played and favourite are OR-ed, play count and last played take the larger value, and the resume
// position is taken only when the item is not played here and has no position of its own. Running the
// same import again changes nothing. It reports whether anything changed.
func (s *Store) MergeUserData(profileID, itemID int64, in ImportedUserData) (bool, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err := ensureUD(tx, profileID, itemID); err != nil {
		return false, err
	}
	var kind string
	if err := tx.QueryRow(`SELECT kind FROM items WHERE id=?`, itemID).Scan(&kind); err != nil {
		return false, err
	}
	var cur UserData
	var last sql.NullInt64
	if err := tx.QueryRow(`SELECT position,played,play_count,last_played_at,favorite FROM user_data WHERE profile_id=? AND item_id=?`,
		profileID, itemID).Scan(&cur.Position, &cur.Played, &cur.PlayCount, &last, &cur.Favorite); err != nil {
		return false, err
	}
	next := cur
	next.Played = cur.Played || in.Played
	next.Favorite = cur.Favorite || in.Favorite
	next.PlayCount = max(cur.PlayCount, in.PlayCount)
	if next.Played && next.PlayCount == 0 {
		next.PlayCount = 1
	}
	lastAt := last.Int64
	if in.LastPlayed > lastAt {
		lastAt = in.LastPlayed
	}
	if next.Played {
		next.Position = 0
	} else if cur.Position == 0 && in.Position > 0 {
		next.Position = in.Position
	}
	changed := next.Played != cur.Played || next.Favorite != cur.Favorite || next.PlayCount != cur.PlayCount ||
		next.Position != cur.Position || lastAt != last.Int64
	if !changed {
		return false, nil
	}
	var lastArg any
	if lastAt > 0 {
		lastArg = lastAt
	}
	if _, err := tx.Exec(`UPDATE user_data SET played=?, favorite=?, play_count=?, position=?, last_played_at=? WHERE profile_id=? AND item_id=?`,
		next.Played, next.Favorite, next.PlayCount, next.Position, lastArg, profileID, itemID); err != nil {
		return false, err
	}
	if next.Played && !cur.Played {
		at := lastAt
		if at == 0 {
			at = now()
		}
		recordWatched(tx, profileID, itemID, at)
		recordEvent(tx, profileID, itemID, at, "import")
	}
	if kind == "movie" {
		if err := mirrorUserData(tx, profileID, itemID); err != nil {
			return false, err
		}
	}
	return true, tx.Commit()
}

// ------------------------------------------------------------------ finding items

// ItemRef is a matched library item.
type ItemRef struct {
	ID    int64  `json:"id"`
	Kind  string `json:"kind"`
	Title string `json:"title"`
	Year  int    `json:"year,omitempty"`
}

func (s *Store) itemRef(q string, args ...any) (*ItemRef, error) {
	rows, err := s.DB.Query(`SELECT i.id, i.kind, i.title, COALESCE(i.year,0) FROM items i WHERE i.missing=0 AND `+q+` ORDER BY i.id LIMIT 2`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ItemRef
	for rows.Next() {
		r := &ItemRef{}
		if err := rows.Scan(&r.ID, &r.Kind, &r.Title, &r.Year); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, ErrNotFound
	}
	return out[0], nil
}

// ErrAmbiguous: more than one item fits a title lookup.
var ErrAmbiguous = errors.New("több elem is illik rá")

// ItemByPath finds a film or episode by its file path.
func (s *Store) ItemByPath(path string) (*ItemRef, error) {
	return s.itemRef(`i.path=? AND i.kind IN ('movie','episode')`, path)
}

// FindByIDs finds a film or series by TMDB or IMDb id (kind: "movie" | "series").
func (s *Store) FindByIDs(kind string, tmdb int64, imdb string) (*ItemRef, string, error) {
	if tmdb > 0 {
		if r, err := s.itemRef(`i.kind=? AND i.tmdb_id=?`, kind, tmdb); err == nil {
			return r, "tmdb", nil
		}
	}
	if imdb = NormalizeImdbID(imdb); imdb != "" {
		if r, err := s.itemRef(`i.kind=? AND i.imdb_id=?`, kind, imdb); err == nil {
			return r, "imdb", nil
		}
	}
	return nil, "", ErrNotFound
}

// FindByTitle finds a film or series by exact title (case-insensitive) and year; only a single hit counts.
func (s *Store) FindByTitle(kind, title string, year int) (*ItemRef, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, ErrNotFound
	}
	q := `i.kind=? AND i.title=? COLLATE NOCASE`
	args := []any{kind, title}
	if year > 0 {
		q += ` AND (i.year=? OR i.year IS NULL)`
		args = append(args, year)
	}
	rows, err := s.DB.Query(`SELECT COUNT(*) FROM items i WHERE i.missing=0 AND `+q, args...)
	if err != nil {
		return nil, err
	}
	var n int
	for rows.Next() {
		rows.Scan(&n)
	}
	rows.Close()
	if n > 1 {
		return nil, ErrAmbiguous
	}
	return s.itemRef(q, args...)
}

// EpisodeOf finds an episode of a series by season and episode number (double episodes included).
func (s *Store) EpisodeOf(seriesID int64, season, episode int) (*ItemRef, error) {
	return s.itemRef(`i.kind='episode' AND i.series_id=? AND i.season_num=? AND (i.episode_num=? OR (i.episode_num<? AND i.episode_end>=?))`,
		seriesID, season, episode, episode, episode)
}

// FirstProfile is the account's oldest profile (where imported state goes).
func (s *Store) FirstProfile(userID int64) (*Profile, error) {
	ps, err := s.Profiles(userID)
	if err != nil {
		return nil, err
	}
	if len(ps) == 0 {
		return nil, ErrNotFound
	}
	first := ps[0]
	for _, p := range ps[1:] {
		if p.ID < first.ID {
			first = p
		}
	}
	return first, nil
}

// ItemRefByID returns a present item.
func (s *Store) ItemRefByID(id int64) (*ItemRef, error) { return s.itemRef(`i.id=?`, id) }

// SeriesUnder returns the series of the present episodes whose files lie below dir; 0 when there are
// none, or episodes of more than one series (a flat "Shows/" folder must not pick one at random).
func (s *Store) SeriesUnder(dir string) int64 {
	var id int64
	sep := "/"
	if strings.Contains(dir, `\`) && !strings.Contains(dir, "/") {
		sep = `\`
	}
	prefix := strings.TrimRight(dir, `/\`) + sep
	var n int
	s.DB.QueryRow(`SELECT COUNT(DISTINCT series_id), MIN(series_id) FROM items WHERE kind='episode' AND missing=0
		AND series_id IS NOT NULL AND substr(path,1,?)=?`, len([]rune(prefix)), prefix).Scan(&n, &id)
	if n != 1 {
		return 0
	}
	return id
}

// ------------------------------------------------------------------ imported configuration

// ImportedID returns what an earlier import made of an external object (source: "radarr@http://…",
// kind: "indexer", "client", "profile", …); 0 when nothing.
func (s *Store) ImportedID(source, kind, externalID string) int64 {
	var id int64
	s.DB.QueryRow(`SELECT local_id FROM import_map WHERE source=? AND kind=? AND external_id=?`, source, kind, externalID).Scan(&id)
	return id
}

// MapImported remembers what an import made of an external object.
func (s *Store) MapImported(source, kind, externalID string, localID int64) error {
	_, err := s.DB.Exec(`INSERT INTO import_map(source,kind,external_id,local_id,created_at) VALUES(?,?,?,?,?)
		ON CONFLICT(source,kind,external_id) DO UPDATE SET local_id=excluded.local_id`, source, kind, externalID, localID, now())
	return err
}
