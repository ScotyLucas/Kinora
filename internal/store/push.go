package store

import (
	"database/sql"
	"slices"
	"strings"
)

// PushIOS is one iOS device paired with the push relay.
type PushIOS struct {
	DeviceID string
	UserID   int64
	Handle   string
	Key      []byte // AES-256 key shared with the device; the relay never sees it
	PushPrefs
	ProfileID int64 // the profile the device's session is on (0 = none chosen)
	IsAdmin   bool  // the account is an admin and the session is not locked to a profile
}

// PushPrefs is what a device asked for: its UI language (notification texts), the TMDB language of
// titles, the categories it turned off and whether it shows live download activities.
type PushPrefs struct {
	Lang     string
	MetaLang string
	Muted    []string
	Live     bool
}

// Wants reports whether the device takes notifications of a category.
func (p PushPrefs) Wants(category string) bool {
	return !slices.Contains(p.Muted, category)
}

const pushCols = `p.device_id,p.user_id,p.handle,p.key,p.lang,p.meta_lang,p.muted,p.live,COALESCE(s.profile_id,0),
 u.is_admin AND s.profile_lock IS NULL`

const pushFrom = ` FROM push_ios p JOIN users u ON u.id=p.user_id LEFT JOIN sessions s ON s.device_id=p.device_id`

func scanPushIOS(row interface{ Scan(...any) error }) (PushIOS, error) {
	var p PushIOS
	var muted string
	err := row.Scan(&p.DeviceID, &p.UserID, &p.Handle, &p.Key, &p.Lang, &p.MetaLang, &muted, &p.Live, &p.ProfileID, &p.IsAdmin)
	if muted != "" {
		p.Muted = strings.Split(muted, ",")
	}
	return p, err
}

func (s *Store) queryPushIOS(where string, args ...any) ([]PushIOS, error) {
	rows, err := s.DB.Query(`SELECT `+pushCols+pushFrom+` `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PushIOS
	for rows.Next() {
		p, err := scanPushIOS(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetPushIOS pairs (or re-pairs) the session's device.
func (s *Store) SetPushIOS(deviceID string, userID int64, handle string, key []byte, prefs PushPrefs) error {
	_, err := s.DB.Exec(`INSERT INTO push_ios(device_id,user_id,handle,key,created_at,lang,meta_lang,muted,live) VALUES(?,?,?,?,?,?,?,?,?)
ON CONFLICT(device_id) DO UPDATE SET user_id=excluded.user_id, handle=excluded.handle, key=excluded.key, lang=excluded.lang,
 meta_lang=excluded.meta_lang, muted=excluded.muted, live=excluded.live`,
		deviceID, userID, handle, key, now(), prefs.Lang, prefs.MetaLang, strings.Join(prefs.Muted, ","), prefs.Live)
	return err
}

// SetPushPrefs changes what a paired device asked for. It reports false when the device is not paired.
func (s *Store) SetPushPrefs(deviceID string, prefs PushPrefs) (bool, error) {
	res, err := s.DB.Exec(`UPDATE push_ios SET lang=?,meta_lang=?,muted=?,live=? WHERE device_id=?`,
		prefs.Lang, prefs.MetaLang, strings.Join(prefs.Muted, ","), prefs.Live, deviceID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if !prefs.Live {
		s.DB.Exec(`DELETE FROM push_live WHERE device_id=?`, deviceID)
	}
	return n > 0, nil
}

// PushIOSFor returns the pairing of one device (nil when unpaired).
func (s *Store) PushIOSFor(deviceID string) (*PushIOS, error) {
	p, err := scanPushIOS(s.DB.QueryRow(`SELECT `+pushCols+pushFrom+` WHERE p.device_id=?`, deviceID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &p, err
}

// PushIOSForUser lists every paired device of an account.
func (s *Store) PushIOSForUser(userID int64) ([]PushIOS, error) {
	return s.queryPushIOS(`WHERE p.user_id=?`, userID)
}

// PushIOSAll lists every paired device.
func (s *Store) PushIOSAll() ([]PushIOS, error) {
	return s.queryPushIOS(`ORDER BY p.device_id`)
}

// DeletePushIOSHandle forgets a handle the relay reported as gone.
func (s *Store) DeletePushIOSHandle(handle string) error {
	_, err := s.DB.Exec(`DELETE FROM push_ios WHERE handle=?`, handle)
	return err
}

// DeletePushIOS unpairs a device and returns its handle ("" when it was not paired).
func (s *Store) DeletePushIOS(deviceID string) (string, error) {
	var h string
	err := s.DB.QueryRow(`DELETE FROM push_ios WHERE device_id=? RETURNING handle`, deviceID).Scan(&h)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return h, err
}

// InboxPrefix starts the handle of a device that fetches its notifications itself (the Android app):
// the relay never sees it, the messages wait in push_inbox.
const InboxPrefix = "inbox:"

// Inbox reports whether the device fetches its notifications instead of getting them through the relay.
func (p PushIOS) Inbox() bool { return strings.HasPrefix(p.Handle, InboxPrefix) }

// maxInbox is how many notifications wait for one device; older ones are dropped.
const maxInbox = 50

// AddPushInbox keeps a notification for a device that fetches them, dropping the oldest past maxInbox.
func (s *Store) AddPushInbox(deviceID string, body []byte) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO push_inbox(device_id,body,created_at) VALUES(?,?,?)`, deviceID, string(body), now()); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM push_inbox WHERE device_id=? AND id NOT IN
 (SELECT id FROM push_inbox WHERE device_id=? ORDER BY id DESC LIMIT ?)`, deviceID, deviceID, maxInbox); err != nil {
		return err
	}
	return tx.Commit()
}

// TakePushInbox returns the device's waiting notifications, oldest first, and forgets them.
func (s *Store) TakePushInbox(deviceID string) ([]string, error) {
	rows, err := s.DB.Query(`DELETE FROM push_inbox WHERE device_id=? RETURNING id, body`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type msg struct {
		id   int64
		body string
	}
	var got []msg
	for rows.Next() {
		var m msg
		if err := rows.Scan(&m.id, &m.body); err != nil {
			return nil, err
		}
		got = append(got, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(got, func(a, b msg) int { return int(a.id - b.id) })
	out := make([]string, len(got))
	for i, m := range got {
		out[i] = m.body
	}
	return out, nil
}

// CountPushIOS is the number of paired iOS devices.
func (s *Store) CountPushIOS() int {
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM push_ios`).Scan(&n)
	return n
}

// PushEvent is a request change waiting to be pushed: a new request (status "pending", for the admins)
// or a status the requester hears about. Triggers on acq_requests write them (migration 18).
type PushEvent struct {
	ID        int64
	RequestID int64
	Status    string
	OldStatus string
	CreatedAt int64
}

// PushEvents returns the oldest waiting events. Events older than since are dropped unsent
// (push was off or the relay was down for too long).
func (s *Store) PushEvents(since int64, limit int) ([]PushEvent, error) {
	s.DB.Exec(`DELETE FROM push_outbox WHERE created_at<?`, since)
	rows, err := s.DB.Query(`SELECT id,request_id,status,old_status,created_at FROM push_outbox ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PushEvent
	for rows.Next() {
		var e PushEvent
		if err := rows.Scan(&e.ID, &e.RequestID, &e.Status, &e.OldStatus, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DonePushEvent removes a sent (or skipped) event.
func (s *Store) DonePushEvent(id int64) {
	s.DB.Exec(`DELETE FROM push_outbox WHERE id=?`, id)
}

// ClearPushEvents drops every waiting event (push is off: nothing to send them to).
func (s *Store) ClearPushEvents() {
	s.DB.Exec(`DELETE FROM push_outbox`)
}

// LiveDownload is a request being downloaded, with its newest grab.
type LiveDownload struct {
	Request  *AcqRequest
	State    string  // downloading | importing
	Progress float64 // 0..1
	ETA      int64   // seconds, -1 = unknown
}

// LiveDownloads lists every request downloading now, with its newest grab's progress.
func (s *Store) LiveDownloads() ([]LiveDownload, error) {
	reqs, err := s.queryRequests(`WHERE r.status=? AND r.user_id IS NOT NULL ORDER BY r.id`, ReqDownloading)
	if err != nil {
		return nil, err
	}
	out := make([]LiveDownload, 0, len(reqs))
	for _, q := range reqs {
		d := LiveDownload{Request: q, State: "downloading", Progress: q.Progress, ETA: -1}
		s.DB.QueryRow(`SELECT status,progress,eta FROM acq_grabs WHERE request_id=? AND status IN ('downloading','importing') ORDER BY id DESC LIMIT 1`,
			q.ID).Scan(&d.State, &d.Progress, &d.ETA)
		out = append(out, d)
	}
	return out, nil
}

// PushLive is the live activity of one request on one device.
type PushLive struct {
	DeviceID  string
	RequestID int64
	StartedAt int64
	SentAt    int64
	Progress  float64
	State     string
	Ended     bool
}

// PushLives lists the live activities the server knows about.
func (s *Store) PushLives() ([]PushLive, error) {
	rows, err := s.DB.Query(`SELECT device_id,request_id,started_at,sent_at,progress,state,ended FROM push_live`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PushLive
	for rows.Next() {
		var l PushLive
		if err := rows.Scan(&l.DeviceID, &l.RequestID, &l.StartedAt, &l.SentAt, &l.Progress, &l.State, &l.Ended); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// SavePushLive stores what was last sent to a live activity.
func (s *Store) SavePushLive(l PushLive) error {
	_, err := s.DB.Exec(`INSERT INTO push_live(device_id,request_id,started_at,sent_at,progress,state,ended) VALUES(?,?,?,?,?,?,?)
ON CONFLICT(device_id,request_id) DO UPDATE SET sent_at=excluded.sent_at, progress=excluded.progress, state=excluded.state, ended=excluded.ended`,
		l.DeviceID, l.RequestID, l.StartedAt, l.SentAt, l.Progress, l.State, l.Ended)
	return err
}

// PrunePushLive forgets ended live activities older than before.
func (s *Store) PrunePushLive(before int64) {
	s.DB.Exec(`DELETE FROM push_live WHERE ended=1 AND sent_at<?`, before)
}

// NewEpisode is an episode added to the library since the last look.
type NewEpisode struct {
	ID       int64
	SeriesID int64
	Season   int
	Episode  int
}

// NewEpisodes returns episodes added after the cursor, up to the first one added after quietSince
// (so a season that is still being scanned arrives in one piece), and the new cursor. Copies of an
// episode the library already had (another version, an upgrade) are left out.
func (s *Store) NewEpisodes(cursor, quietSince int64) ([]NewEpisode, int64, error) {
	var stop sql.NullInt64
	s.DB.QueryRow(`SELECT MIN(id) FROM items WHERE id>? AND kind='episode' AND added_at>?`, cursor, quietSince).Scan(&stop)
	where := `i.id>? AND i.kind='episode' AND i.missing=0 AND i.series_id IS NOT NULL`
	args := []any{cursor}
	if stop.Valid {
		where += ` AND i.id<?`
		args = append(args, stop.Int64)
	}
	rows, err := s.DB.Query(`SELECT i.id,i.series_id,COALESCE(i.season_num,0),COALESCE(i.episode_num,0) FROM items i WHERE `+where+`
 AND NOT EXISTS(SELECT 1 FROM items o WHERE o.id<i.id AND o.kind='episode' AND o.series_id=i.series_id
   AND o.season_num IS i.season_num AND o.episode_num IS i.episode_num AND i.episode_num IS NOT NULL)
 ORDER BY i.id LIMIT 500`, args...)
	if err != nil {
		return nil, cursor, err
	}
	defer rows.Close()
	var out []NewEpisode
	for rows.Next() {
		var e NewEpisode
		if err := rows.Scan(&e.ID, &e.SeriesID, &e.Season, &e.Episode); err != nil {
			return nil, cursor, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, cursor, err
	}
	// the cursor moves past everything seen, copies included
	next := cursor
	q := `SELECT COALESCE(MAX(id),?) FROM items WHERE id>?`
	qa := []any{cursor, cursor}
	if stop.Valid {
		q += ` AND id<?`
		qa = append(qa, stop.Int64)
	}
	if len(out) == 500 {
		q += ` AND id<=?`
		qa = append(qa, out[len(out)-1].ID)
	}
	s.DB.QueryRow(q, qa...).Scan(&next)
	return out, next, nil
}

// SeriesFollowers returns the profiles that follow a series: they marked it favourite or watched
// (some of) an episode, and did not hide it.
func (s *Store) SeriesFollowers(seriesID int64) ([]int64, error) {
	rows, err := s.DB.Query(`SELECT DISTINCT d.profile_id FROM user_data d JOIN items i ON i.id=d.item_id
WHERE ((i.id=? AND d.favorite=1) OR (i.series_id=? AND i.kind='episode' AND (d.played=1 OR d.position>0)))
  AND NOT EXISTS(SELECT 1 FROM user_data h WHERE h.profile_id=d.profile_id AND h.item_id=? AND h.hidden=1)`, seriesID, seriesID, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// LibraryItemByTmdb returns the id of the movie or series with a TMDB id (0 = none).
func (s *Store) LibraryItemByTmdb(kind string, tmdbID int64) int64 {
	var id int64
	s.DB.QueryRow(`SELECT id FROM items WHERE kind=? AND tmdb_id=? AND missing=0 ORDER BY id LIMIT 1`, kind, tmdbID).Scan(&id)
	return id
}
