package server

import (
	"fmt"
	"io/fs"
	"net/http"

	"github.com/rakunlabs/ada"
	"github.com/rakunlabs/ada/handler/folder"
	ui "github.com/rakunlabs/calendar/_ui"
)

func registerUI(s *ada.Server) error {
	dist, err := fs.Sub(ui.Dist, "dist")
	if err != nil {
		return fmt.Errorf("embedded UI: %w", err)
	}
	files, err := folder.New(&folder.Config{
		PrefixPath: "/calendar", Index: true, StripIndexName: true,
		CacheRegex: []*folder.RegexCacheStore{
			{Regex: `^index\.html$`, CacheControl: "no-cache"},
			{Regex: `.*\.(js|css|woff2)$`, CacheControl: "public, max-age=31536000, immutable"},
		},
	})
	if err != nil {
		return fmt.Errorf("UI folder handler: %w", err)
	}
	files.SetFs(http.FS(dist))
	// Explicit static routes keep unknown API routes from returning the SPA HTML.
	for _, path := range []string{"/calendar/", "/calendar/index.html", "/calendar/assets/*"} {
		s.GET(path, files.ServeHTTP)
		s.HEAD(path, files.ServeHTTP)
	}
	for _, path := range []string{"/", "/calendar"} {
		s.GET(path, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/calendar/", http.StatusTemporaryRedirect)
		})
	}
	return nil
}
