package store

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------- passwords

const pbkdf2Iter = 210_000

// HashPassword returns "pbkdf2-sha256$iter$salt$hash" (base64, raw std).
func HashPassword(pw string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key, _ := pbkdf2.Key(sha256.New, pw, salt, pbkdf2Iter, 32)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iter, enc.EncodeToString(salt), enc.EncodeToString(key))
}

// CheckPassword verifies pw against an encoded hash in constant time.
func CheckPassword(encoded, pw string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash is checked when a username does not exist, so response timing
// does not reveal which accounts exist.
var dummyHash = HashPassword("kinora-dummy-password")

// DummyCheck burns the same CPU as a real password check.
func DummyCheck(pw string) { CheckPassword(dummyHash, pw) }

// RandomToken returns n random bytes, hex encoded.
func RandomToken(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// RandomCode returns an n-character code from an unambiguous alphabet.
func RandomCode(n int) string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// ---------------------------------------------------------------- users

// User is an account (a household). Each user has up to MaxProfiles profiles.
type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	IsAdmin   bool   `json:"is_admin"`
	CreatedAt int64  `json:"created_at"`
	// Libraries are the only libraries the account may see; nil = all of them.
	Libraries []int64 `json:"libraries"`
	passHash  string
}

// MaxProfiles per account.
const MaxProfiles = 8

var ErrExists = errors.New("ez a felhasználónév már foglalt")

// CountUsers returns the number of accounts.
func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// ErrSetupDone is returned by CreateFirstAdmin when an account already exists.
var ErrSetupDone = errors.New("a szerver már be van állítva")

// CreateFirstAdmin creates the initial admin atomically (no race between two setup requests).
func (s *Store) CreateFirstAdmin(username, password string) (*User, error) {
	tx, err := s.DB.Begin() // _txlock=immediate: takes the write lock up front
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, ErrSetupDone
	}
	u, err := createUserTx(tx, username, password, true)
	if err != nil {
		return nil, err
	}
	return u, tx.Commit()
}

// CreateUser creates an account plus a default profile named after it.
func (s *Store) CreateUser(username, password string, admin bool) (*User, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	u, err := createUserTx(tx, username, password, admin)
	if err != nil {
		return nil, err
	}
	return u, tx.Commit()
}

func createUserTx(tx *sql.Tx, username, password string, admin bool) (*User, error) {
	var exists int
	tx.QueryRow(`SELECT COUNT(*) FROM users WHERE username=?`, username).Scan(&exists)
	if exists > 0 {
		return nil, ErrExists
	}
	t := now()
	res, err := tx.Exec(`INSERT INTO users(username,pass_hash,is_admin,created_at) VALUES(?,?,?,?)`,
		username, HashPassword(password), admin, t)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if _, err := tx.Exec(`INSERT INTO profiles(user_id,name,color,sub_langs,audio_langs,created_at) VALUES(?,?,?,?,?,?)`,
		id, username, "#c97c5d", "hun,eng", "hun,eng", t); err != nil {
		return nil, err
	}
	return &User{ID: id, Username: username, IsAdmin: admin, CreatedAt: t}, nil
}

const userCols = `id, username, pass_hash, is_admin, created_at, libraries`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	u := &User{}
	var libs string
	if err := row.Scan(&u.ID, &u.Username, &u.passHash, &u.IsAdmin, &u.CreatedAt, &libs); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	u.Libraries = parseLibIDs(libs)
	return u, nil
}

// UserByID loads a user.
func (s *Store) UserByID(id int64) (*User, error) {
	return scanUser(s.DB.QueryRow(`SELECT `+userCols+` FROM users WHERE id=?`, id))
}

// Authenticate checks credentials; it always spends a full hash computation.
func (s *Store) Authenticate(username, password string) (*User, error) {
	u, err := scanUser(s.DB.QueryRow(`SELECT `+userCols+` FROM users WHERE username=?`, username))
	if err != nil {
		DummyCheck(password)
		return nil, ErrNotFound
	}
	if !CheckPassword(u.passHash, password) {
		return nil, ErrNotFound
	}
	return u, nil
}

// CheckUserPassword verifies a user's current password.
func (s *Store) CheckUserPassword(u *User, password string) bool {
	fresh, err := s.UserByID(u.ID)
	if err != nil {
		return false
	}
	return CheckPassword(fresh.passHash, password)
}

// ListUsers returns all users.
func (s *Store) ListUsers() ([]*User, error) {
	rows, err := s.DB.Query(`SELECT ` + userCols + ` FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetPassword changes a password and logs out all other sessions of that user.
func (s *Store) SetPassword(userID int64, password, keepToken string) error {
	if _, err := s.DB.Exec(`UPDATE users SET pass_hash=? WHERE id=?`, HashPassword(password), userID); err != nil {
		return err
	}
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE user_id=? AND token<>?`, userID, hashToken(keepToken))
	return err
}

// SetUserLibraries limits an account to the given libraries; nil means all of them.
// Admins always see everything (the limit is ignored for them).
func (s *Store) SetUserLibraries(userID int64, ids []int64) error {
	v := ""
	if ids != nil {
		parts := make([]string, 0, len(ids))
		for _, id := range ids {
			parts = append(parts, strconv.FormatInt(id, 10))
		}
		v = strings.Join(parts, ",")
		if v == "" {
			v = "0" // restricted to nothing
		}
	}
	_, err := s.DB.Exec(`UPDATE users SET libraries=? WHERE id=?`, v, userID)
	return err
}

func parseLibIDs(v string) []int64 {
	if v == "" {
		return nil
	}
	out := []int64{}
	for _, f := range strings.Split(v, ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(f), 10, 64); err == nil && id > 0 {
			out = append(out, id)
		}
	}
	return out
}

// SetAdmin toggles admin flag.
func (s *Store) SetAdmin(userID int64, admin bool) error {
	_, err := s.DB.Exec(`UPDATE users SET is_admin=? WHERE id=?`, admin, userID)
	return err
}

// DeleteUser removes an account (cascades profiles, sessions, user data).
func (s *Store) DeleteUser(id int64) error {
	_, err := s.DB.Exec(`DELETE FROM users WHERE id=?`, id)
	return err
}

// ---------------------------------------------------------------- sessions

// SessionTTL is the sliding session lifetime.
const SessionTTL = 30 * 24 * time.Hour

// CreateSession returns a new opaque token (only its hash is stored) for a web session.
func (s *Store) CreateSession(userID int64) (string, error) {
	tok, _, err := s.CreateDeviceSession(userID, DeviceInfo{Platform: "web"}, 0, 0)
	return tok, err
}

// Session resolves a token to user and (optional) selected profile.
func (s *Store) Session(tok string) (*User, *Profile, error) {
	ss, err := s.SessionFull(tok, "")
	if err != nil {
		return nil, nil, err
	}
	return ss.User, ss.Profile, nil
}

// SetSessionProfile selects (or clears, with 0) the active profile of a session.
func (s *Store) SetSessionProfile(tok string, profileID int64) error {
	var v any
	if profileID > 0 {
		v = profileID
	}
	_, err := s.DB.Exec(`UPDATE sessions SET profile_id=? WHERE token=?`, v, hashToken(tok))
	return err
}

// DeleteSession logs out.
func (s *Store) DeleteSession(tok string) error {
	_, err := s.DB.Exec(`DELETE FROM sessions WHERE token=?`, hashToken(tok))
	return err
}

// PurgeSessions removes expired sessions.
func (s *Store) PurgeSessions() {
	s.DB.Exec(`DELETE FROM sessions WHERE expires_at<?`, now())
}

// ---------------------------------------------------------------- profiles

// Profile is a household member (Netflix-style).
type Profile struct {
	ID             int64  `json:"id"`
	UserID         int64  `json:"user_id"`
	Name           string `json:"name"`
	Color          string `json:"color"`
	HasPIN         bool   `json:"has_pin"`
	IsKids         bool   `json:"is_kids"`
	MaxRatingLevel *int   `json:"max_rating_level"`
	SubLangs       string `json:"sub_langs"`
	AudioLangs     string `json:"audio_langs"`
	SubMode        string `json:"sub_mode"`
	AutoplayNext   bool   `json:"autoplay_next"`
	MaxBitrate     int    `json:"max_bitrate"`
	SkipIntro      string `json:"skip_intro"` // manual | auto | off
	Snow           bool   `json:"snow"`       // snowflakes fall on the screens outside the player
	CreatedAt      int64  `json:"created_at"`
	pinHash        string
	// Libraries limits the profile to the account's allowed libraries (nil = all).
	// Loaded with the profile from users.libraries, never sent to clients.
	Libraries []int64 `json:"-"`
	// MetaLang is the TMDB language the current request wants titles and overviews in
	// ("" = the stored metadata_lang ones). Set per request, never stored.
	MetaLang string `json:"-"`
}

func metaLang(p *Profile) string {
	if p == nil {
		return ""
	}
	return p.MetaLang
}

// RatingLimit returns the maximum allowed rating_level (-1 = unlimited) and
// whether items with unknown rating must be excluded (kids profiles).
func (p *Profile) RatingLimit() (limit int, strict bool) {
	if p == nil {
		return -1, false
	}
	if p.IsKids {
		if p.MaxRatingLevel != nil {
			return *p.MaxRatingLevel, true
		}
		return 1, true
	}
	if p.MaxRatingLevel != nil {
		return *p.MaxRatingLevel, false
	}
	return -1, false
}

// CheckPIN verifies a profile PIN (true when the profile has no PIN).
func (p *Profile) CheckPIN(pin string) bool {
	if p.pinHash == "" {
		return true
	}
	return CheckPassword(p.pinHash, pin)
}

const profileCols = `id,user_id,name,color,COALESCE(pin_hash,''),is_kids,max_rating_level,sub_langs,audio_langs,sub_mode,autoplay_next,max_bitrate,skip_intro,snow,created_at,
	COALESCE((SELECT u.libraries FROM users u WHERE u.id=profiles.user_id AND u.is_admin=0),'')`

func scanProfile(row interface{ Scan(...any) error }) (*Profile, error) {
	p := &Profile{}
	var lvl sql.NullInt64
	var libs string
	if err := row.Scan(&p.ID, &p.UserID, &p.Name, &p.Color, &p.pinHash, &p.IsKids, &lvl,
		&p.SubLangs, &p.AudioLangs, &p.SubMode, &p.AutoplayNext, &p.MaxBitrate, &p.SkipIntro, &p.Snow, &p.CreatedAt, &libs); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if lvl.Valid {
		v := int(lvl.Int64)
		p.MaxRatingLevel = &v
	}
	p.HasPIN = p.pinHash != ""
	p.Libraries = parseLibIDs(libs)
	return p, nil
}

// Profile loads one profile.
func (s *Store) Profile(id int64) (*Profile, error) {
	return scanProfile(s.DB.QueryRow(`SELECT `+profileCols+` FROM profiles WHERE id=?`, id))
}

// Profiles lists the profiles of an account.
func (s *Store) Profiles(userID int64) ([]*Profile, error) {
	rows, err := s.DB.Query(`SELECT `+profileCols+` FROM profiles WHERE user_id=? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Profile{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProfileInput is the editable part of a profile. Nil fields are left unchanged.
type ProfileInput struct {
	Name           *string `json:"name"`
	Color          *string `json:"color"`
	PIN            *string `json:"pin"` // "" removes the PIN
	IsKids         *bool   `json:"is_kids"`
	MaxRatingLevel *int    `json:"max_rating_level"` // -1 = clear
	SubLangs       *string `json:"sub_langs"`
	AudioLangs     *string `json:"audio_langs"`
	SubMode        *string `json:"sub_mode"`
	AutoplayNext   *bool   `json:"autoplay_next"`
	MaxBitrate     *int    `json:"max_bitrate"`
	SkipIntro      *string `json:"skip_intro"`
	Snow           *bool   `json:"snow"`
}

// Validate checks the input and returns a Hungarian error message.
func (in *ProfileInput) Validate() error {
	if in.Name != nil {
		n := strings.TrimSpace(*in.Name)
		if n == "" || len([]rune(n)) > 30 {
			return errors.New("a profil neve 1–30 karakter lehet")
		}
		in.Name = &n
	}
	if in.PIN != nil && *in.PIN != "" {
		if len(*in.PIN) < 4 || len(*in.PIN) > 8 || strings.Trim(*in.PIN, "0123456789") != "" {
			return errors.New("a PIN 4–8 számjegyből állhat")
		}
	}
	if in.SubMode != nil && *in.SubMode != "auto" && *in.SubMode != "always" && *in.SubMode != "off" {
		return errors.New("érvénytelen feliratmód")
	}
	if in.MaxRatingLevel != nil && (*in.MaxRatingLevel < -1 || *in.MaxRatingLevel > 4) {
		return errors.New("érvénytelen korhatár-szint")
	}
	if in.SkipIntro != nil && *in.SkipIntro != "manual" && *in.SkipIntro != "auto" && *in.SkipIntro != "off" {
		return errors.New("érvénytelen intró-átugrási mód")
	}
	if in.MaxBitrate != nil && (*in.MaxBitrate < 0 || *in.MaxBitrate > 200000) {
		return errors.New("érvénytelen bitráta")
	}
	if in.Color != nil && (len(*in.Color) != 7 || (*in.Color)[0] != '#') {
		return errors.New("érvénytelen szín")
	}
	for _, l := range []*string{in.SubLangs, in.AudioLangs} {
		if l != nil {
			*l = NormalizeLangList(*l)
		}
	}
	return nil
}

// NormalizeLangList cleans "hun, ENG" → "hun,eng".
func NormalizeLangList(s string) string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" && len(p) <= 8 {
			out = append(out, p)
		}
	}
	return strings.Join(out, ",")
}

// CreateProfile adds a profile (max MaxProfiles per account).
func (s *Store) CreateProfile(userID int64, in ProfileInput) (*Profile, error) {
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM profiles WHERE user_id=?`, userID).Scan(&n)
	if n >= MaxProfiles {
		return nil, fmt.Errorf("legfeljebb %d profil lehet egy fiókban", MaxProfiles)
	}
	if in.Name == nil {
		return nil, errors.New("a profil neve kötelező")
	}
	res, err := s.DB.Exec(`INSERT INTO profiles(user_id,name,sub_langs,audio_langs,created_at) VALUES(?,?,?,?,?)`,
		userID, *in.Name, "hun,eng", "hun,eng", now())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if err := s.UpdateProfile(id, in); err != nil {
		return nil, err
	}
	return s.Profile(id)
}

// UpdateProfile applies non-nil fields.
func (s *Store) UpdateProfile(id int64, in ProfileInput) error {
	set := []string{}
	args := []any{}
	add := func(col string, v any) { set = append(set, col+"=?"); args = append(args, v) }
	if in.Name != nil {
		add("name", *in.Name)
	}
	if in.Color != nil {
		add("color", *in.Color)
	}
	if in.PIN != nil {
		if *in.PIN == "" {
			add("pin_hash", nil)
		} else {
			add("pin_hash", HashPassword(*in.PIN))
		}
	}
	if in.IsKids != nil {
		add("is_kids", *in.IsKids)
	}
	if in.MaxRatingLevel != nil {
		if *in.MaxRatingLevel < 0 {
			add("max_rating_level", nil)
		} else {
			add("max_rating_level", *in.MaxRatingLevel)
		}
	}
	if in.SubLangs != nil {
		add("sub_langs", *in.SubLangs)
	}
	if in.AudioLangs != nil {
		add("audio_langs", *in.AudioLangs)
	}
	if in.SubMode != nil {
		add("sub_mode", *in.SubMode)
	}
	if in.AutoplayNext != nil {
		add("autoplay_next", *in.AutoplayNext)
	}
	if in.MaxBitrate != nil {
		add("max_bitrate", *in.MaxBitrate)
	}
	if in.SkipIntro != nil {
		add("skip_intro", *in.SkipIntro)
	}
	if in.Snow != nil {
		add("snow", *in.Snow)
	}
	if len(set) == 0 {
		return nil
	}
	args = append(args, id)
	_, err := s.DB.Exec(`UPDATE profiles SET `+strings.Join(set, ",")+` WHERE id=?`, args...)
	return err
}

// DeleteProfile removes a profile; the last profile of an account cannot be removed.
func (s *Store) DeleteProfile(p *Profile) error {
	var n int
	s.DB.QueryRow(`SELECT COUNT(*) FROM profiles WHERE user_id=?`, p.UserID).Scan(&n)
	if n <= 1 {
		return errors.New("az utolsó profil nem törölhető")
	}
	_, err := s.DB.Exec(`DELETE FROM profiles WHERE id=?`, p.ID)
	return err
}

// ---------------------------------------------------------------- invites

// Invite lets someone self-register.
type Invite struct {
	Code      string `json:"code"`
	CreatedBy int64  `json:"created_by"`
	Note      string `json:"note"`
	UsesLeft  int    `json:"uses_left"`
	ExpiresAt *int64 `json:"expires_at"`
	CreatedAt int64  `json:"created_at"`
}

// Valid reports whether the invite can still be used.
func (i *Invite) Valid() bool {
	return i.UsesLeft > 0 && (i.ExpiresAt == nil || *i.ExpiresAt > now())
}

// CreateInvite makes a new invite. hours<=0 means no expiry.
func (s *Store) CreateInvite(by int64, note string, uses, hours int) (*Invite, error) {
	if uses < 1 {
		uses = 1
	}
	inv := &Invite{Code: RandomCode(10), CreatedBy: by, Note: note, UsesLeft: uses, CreatedAt: now()}
	if hours > 0 {
		e := now() + int64(hours)*3600
		inv.ExpiresAt = &e
	}
	_, err := s.DB.Exec(`INSERT INTO invites(code,created_by,note,uses_left,expires_at,created_at) VALUES(?,?,?,?,?,?)`,
		inv.Code, by, note, uses, inv.ExpiresAt, inv.CreatedAt)
	return inv, err
}

func scanInvite(row interface{ Scan(...any) error }) (*Invite, error) {
	i := &Invite{}
	var by sql.NullInt64
	var exp sql.NullInt64
	if err := row.Scan(&i.Code, &by, &i.Note, &i.UsesLeft, &exp, &i.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	i.CreatedBy = by.Int64
	if exp.Valid {
		i.ExpiresAt = &exp.Int64
	}
	return i, nil
}

const inviteCols = `code,created_by,note,uses_left,expires_at,created_at`

// Invite loads an invite by code (case-insensitive).
func (s *Store) Invite(code string) (*Invite, error) {
	return scanInvite(s.DB.QueryRow(`SELECT `+inviteCols+` FROM invites WHERE code=?`, strings.ToUpper(strings.TrimSpace(code))))
}

// Invites lists all invites.
func (s *Store) Invites() ([]*Invite, error) {
	rows, err := s.DB.Query(`SELECT ` + inviteCols + ` FROM invites ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Invite{}
	for rows.Next() {
		i, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// DeleteInvite revokes an invite.
func (s *Store) DeleteInvite(code string) error {
	_, err := s.DB.Exec(`DELETE FROM invites WHERE code=?`, code)
	return err
}

// ErrInviteInvalid is returned for used up / expired / unknown invites.
var ErrInviteInvalid = errors.New("a meghívó érvénytelen vagy lejárt")

// RedeemInvite atomically consumes one use and creates the account.
func (s *Store) RedeemInvite(code, username, password string) (*User, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	inv, err := scanInvite(tx.QueryRow(`SELECT `+inviteCols+` FROM invites WHERE code=?`, strings.ToUpper(strings.TrimSpace(code))))
	if err != nil || !inv.Valid() {
		return nil, ErrInviteInvalid
	}
	u, err := createUserTx(tx, username, password, false)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE invites SET uses_left=uses_left-1 WHERE code=?`, inv.Code); err != nil {
		return nil, err
	}
	return u, tx.Commit()
}
