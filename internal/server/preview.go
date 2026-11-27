package server

import (
	"bytes"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ScotyLucas/kinora/internal/scanner"
)

// Link previews: chat apps (iMessage, WhatsApp, Discord, Slack…) fetch a shared link
// without cookies and without the #fragment, and show the page's Open Graph tags.
// The web client routes in the fragment, so the shared links use a path
// (/party/CODE, /join/CODE): the server answers it with the preview's tags and sends
// the browser on to the app's own route (#/party/CODE, #/join/CODE).

// linkCode is what a party or invite code may look like in a link.
var linkCode = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

const previewDesc = "Saját médiaszerver filmekhez és sorozatokhoz"

type preview struct {
	Title, Desc, Image string
	Large              bool // a wide picture rather than an icon
}

// absBase is the server's address for the absolute URLs Open Graph needs: the public
// address when it is set, otherwise the address the request came to.
func (s *Server) absBase(r *http.Request) string {
	if u := strings.TrimRight(strings.TrimSpace(s.st.Setting("public_url")), "/"); u != "" {
		return u
	}
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) siteName() string {
	if n := strings.TrimSpace(s.st.Setting("server_name")); n != "" {
		return n
	}
	return "Kinora"
}

// iconPath is the server's own icon (the uploaded brand icon, else Kinora's), relative to the root.
func (s *Server) iconPath() string {
	if u := s.brandURL("icon-512.png"); u != "" {
		return strings.TrimPrefix(u, "/")
	}
	return "icons/icon-512.png"
}

func (s *Server) genericPreview() preview {
	return preview{Title: s.siteName(), Desc: previewDesc, Image: s.iconPath()}
}

// metaTags renders the Open Graph and Twitter tags; every value is escaped.
func (s *Server) metaTags(r *http.Request, p preview) string {
	base := s.absBase(r)
	e := html.EscapeString
	card := "summary"
	if p.Large {
		card = "summary_large_image"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<meta property=\"og:type\" content=\"website\">\n")
	fmt.Fprintf(&b, "<meta property=\"og:site_name\" content=\"%s\">\n", e(s.siteName()))
	fmt.Fprintf(&b, "<meta property=\"og:title\" content=\"%s\">\n", e(p.Title))
	fmt.Fprintf(&b, "<meta property=\"og:description\" content=\"%s\">\n", e(p.Desc))
	fmt.Fprintf(&b, "<meta name=\"description\" content=\"%s\">\n", e(p.Desc))
	fmt.Fprintf(&b, "<meta property=\"og:image\" content=\"%s\">\n", e(base+"/"+p.Image))
	fmt.Fprintf(&b, "<meta name=\"twitter:card\" content=\"%s\">\n", card)
	return b.String()
}

// indexWithMeta is index.html with the server's preview tags before </head>.
func (s *Server) indexWithMeta(root fs.FS, r *http.Request) ([]byte, error) {
	page, err := fs.ReadFile(root, "index.html")
	if err != nil {
		return nil, err
	}
	tags := s.metaTags(r, s.genericPreview())
	if i := bytes.Index(page, []byte("</head>")); i >= 0 {
		out := make([]byte, 0, len(page)+len(tags))
		out = append(out, page[:i]...)
		out = append(out, tags...)
		return append(out, page[i:]...), nil
	}
	return page, nil
}

// writePreview answers a shared link: the tags, and an instant redirect to the app's route.
// The redirect is relative, so it also works when the server sits under a path prefix.
func (s *Server) writePreview(w http.ResponseWriter, r *http.Request, p preview, route string) {
	target := "../#/" + route
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"hu\">\n<head>\n<meta charset=\"utf-8\">\n")
	fmt.Fprintf(&b, "<title>%s</title>\n", html.EscapeString(p.Title))
	b.WriteString(s.metaTags(r, p))
	fmt.Fprintf(&b, "<meta http-equiv=\"refresh\" content=\"0;url=%s\">\n", html.EscapeString(target))
	b.WriteString("<meta name=\"robots\" content=\"noindex\">\n</head>\n<body>\n")
	fmt.Fprintf(&b, "<p><a href=\"%s\">%s</a></p>\n</body>\n</html>\n", html.EscapeString(target), html.EscapeString(p.Title))
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Robots-Tag", "noindex")
	h.Set("Content-Security-Policy", "default-src 'none'; img-src 'self'")
	w.Write([]byte(b.String()))
}

// GET /party/{code} – a watch party link: the title and poster of what the party watches,
// while the party is open. An unknown or closed party gets the server's own preview.
func (s *Server) handlePartyPreview(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	code := r.PathValue("code")
	if !linkCode.MatchString(code) {
		s.writePreview(w, r, s.genericPreview(), "home")
		return
	}
	code = strings.ToUpper(code)
	p := s.genericPreview()
	if room := s.Party.Get(code); room != nil {
		if title := s.partyTitle(room.Info().ItemID); title != "" {
			p.Title = "Watch party: " + title
			p.Desc = "Közös nézés a(z) " + s.siteName() + " szerveren – koppints a csatlakozáshoz."
			if s.partyPoster(room.Info().ItemID) != "" {
				p.Image = "party/" + code + "/poster.jpg"
				p.Large = true
			}
		}
	}
	s.writePreview(w, r, p, "party/"+code)
}

// GET /party/{code}/poster.jpg – the poster in a party link's preview, only while the party is open.
func (s *Server) handlePartyPoster(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	room := s.Party.Get(strings.ToUpper(r.PathValue("code")))
	if room == nil {
		writeErr(w, http.StatusNotFound, "nem található")
		return
	}
	p := s.partyPoster(room.Info().ItemID)
	if p == "" {
		writeErr(w, http.StatusNotFound, "nincs kép")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "image/jpeg")
	http.ServeFile(w, r, p)
}

// partyTitle is how a party's film or episode is named in its preview ("" if it is gone).
func (s *Server) partyTitle(id int64) string {
	it, err := s.st.Item(id, nil)
	if err != nil {
		return ""
	}
	if it.Kind == "episode" && it.SeriesTitle != "" {
		if it.SeasonNum != nil && it.EpisodeNum != nil {
			return fmt.Sprintf("%s · S%02dE%02d", it.SeriesTitle, *it.SeasonNum, *it.EpisodeNum)
		}
		return it.SeriesTitle
	}
	if it.Year != nil && *it.Year > 0 {
		return fmt.Sprintf("%s (%d)", it.Title, *it.Year)
	}
	return it.Title
}

// partyPoster is the poster file of a party's item, or of its series ("" if none).
func (s *Server) partyPoster(id int64) string {
	ids := []int64{id}
	if _, sid := s.st.ItemKindSeries(id); sid > 0 && sid != id {
		ids = append(ids, sid)
	}
	for _, iid := range ids {
		p := filepath.Join(scanner.ImageDir(s.ImgDir, iid), "poster.jpg")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// GET /join/{code} – an invite link: the server's name, nothing about who it is for.
func (s *Server) handleJoinPreview(w http.ResponseWriter, r *http.Request, c *reqCtx) {
	code := r.PathValue("code")
	if !linkCode.MatchString(code) {
		s.writePreview(w, r, s.genericPreview(), "home")
		return
	}
	p := s.genericPreview()
	if inv, err := s.st.Invite(code); err == nil && inv.Valid() {
		p.Title = "Meghívó: " + s.siteName()
		p.Desc = "Meghívtak a(z) " + s.siteName() + " médiaszerverre – nyisd meg a fiókod elkészítéséhez."
	}
	s.writePreview(w, r, p, "join/"+code)
}
